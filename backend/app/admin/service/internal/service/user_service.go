package service

import (
	"context"

	"entgo.io/ent/dialect/sql"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/go-utils/trans"
	pagination "github.com/tx7do/kratos-bootstrap/api/gen/go/pagination/v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"kratos-admin/app/admin/service/internal/data"

	adminV1 "kratos-admin/api/gen/go/admin/service/v1"
	authenticationV1 "kratos-admin/api/gen/go/authentication/service/v1"
	userV1 "kratos-admin/api/gen/go/user/service/v1"

	"kratos-admin/pkg/middleware/auth"
)

type UserService struct {
	adminV1.UserServiceHTTPServer

	log *log.Helper

	userRepo            *data.UserRepo
	roleRepo            *data.RoleRepo
	userCredentialsRepo *data.UserCredentialRepo
	positionRepo        *data.PositionRepo
	departmentRepo      *data.DepartmentRepo
	organizationRepo    *data.OrganizationRepo
}

func NewUserService(
	logger log.Logger,
	userRepo *data.UserRepo,
	roleRepo *data.RoleRepo,
	userCredentialsRepo *data.UserCredentialRepo,
	positionRepo *data.PositionRepo,
	departmentRepo *data.DepartmentRepo,
	organizationRepo *data.OrganizationRepo,
) *UserService {
	l := log.NewHelper(log.With(logger, "module", "user/service/admin-service"))
	svc := &UserService{
		log:                 l,
		userRepo:            userRepo,
		roleRepo:            roleRepo,
		userCredentialsRepo: userCredentialsRepo,
		positionRepo:        positionRepo,
		departmentRepo:      departmentRepo,
		organizationRepo:    organizationRepo,
	}

	svc.init()

	return svc
}

func (s *UserService) init() {
	ctx := context.Background()
	if count, _ := s.userRepo.Count(ctx, []func(s *sql.Selector){}); count == 0 {
		_ = s.CreateDefaultUser(ctx)
	}
}

func (s *UserService) List(ctx context.Context, req *pagination.PagingRequest) (*userV1.ListUserResponse, error) {
	return s.userRepo.List(ctx, req)
}

func (s *UserService) Get(ctx context.Context, req *userV1.GetUserRequest) (*userV1.User, error) {
	user, err := s.userRepo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	//role, err := s.roleRepo.Get(ctx, user.GetRoleId())
	//if err == nil && role != nil {
	//	user.Roles = append(user.Roles, role.GetCode())
	//}

	return user, nil
}

func (s *UserService) GetUserByUserName(ctx context.Context, req *userV1.GetUserByUserNameRequest) (*userV1.User, error) {
	user, err := s.userRepo.GetUserByUserName(ctx, req.GetUsername())
	if err != nil {
		return nil, err
	}

	//role, err := s.roleRepo.Get(ctx, user.GetRoleId())
	//if err == nil && role != nil {
	//	user.Roles = append(user.Roles, role.GetCode())
	//}

	return user, nil
}

func (s *UserService) Create(ctx context.Context, req *userV1.CreateUserRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return &emptypb.Empty{}, err
	}

	// 获取操作者的用户信息
	operatorUser, err := s.userRepo.Get(ctx, operator.UserId)
	if err != nil {
		return nil, err
	}

	// 校验操作者的权限
	if operatorUser.GetAuthority() != userV1.UserAuthority_SYS_ADMIN {
		return nil, adminV1.ErrorForbidden("权限不够")
	}

	if req.Data.Authority == nil {
		req.Data.Authority = userV1.UserAuthority_CUSTOMER_USER.Enum()
	}

	if req.Data.Authority != nil {
		if operatorUser.GetAuthority() < req.Data.GetAuthority() {
			return nil, adminV1.ErrorForbidden("不能够创建同级用户或者比自己权限高的用户")
		}
	}

	req.Data.CreateBy = trans.Ptr(operator.UserId)

	// 创建用户
	var user *userV1.User
	if user, err = s.userRepo.Create(ctx, req); err != nil {
		s.log.Error(err)
		return nil, err
	}

	if len(req.GetPassword()) == 0 {
		// 如果没有设置密码，则默认设置为 666666
		req.Password = trans.Ptr("666666")
	}

	if len(req.GetPassword()) > 0 {
		if err = s.userCredentialsRepo.Create(ctx, &authenticationV1.CreateUserCredentialRequest{
			Data: &authenticationV1.UserCredential{
				UserId:   user.Id,
				TenantId: user.TenantId,

				IdentityType: authenticationV1.IdentityType_USERNAME.Enum(),
				Identifier:   req.Data.Username,

				CredentialType: authenticationV1.CredentialType_PASSWORD_HASH.Enum(),
				Credential:     req.Password,

				IsPrimary: trans.Ptr(true),
				Status:    authenticationV1.UserCredentialStatus_ENABLED.Enum(),
			},
		}); err != nil {
			return nil, err
		}
	}

	return &emptypb.Empty{}, nil
}

func (s *UserService) Update(ctx context.Context, req *userV1.UpdateUserRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return &emptypb.Empty{}, err
	}

	// 获取操作者的用户信息
	operatorUser, err := s.userRepo.Get(ctx, operator.UserId)
	if err != nil {
		return nil, err
	}

	// 校验操作者的权限
	if operatorUser.GetAuthority() != userV1.UserAuthority_SYS_ADMIN {
		return nil, adminV1.ErrorForbidden("权限不够")
	}

	if req.Data.Authority != nil {
		if operatorUser.GetAuthority() < req.Data.GetAuthority() {
			return nil, adminV1.ErrorForbidden("不能够赋权同级用户或者比自己权限高的用户")
		}
	}

	req.Data.UpdateBy = trans.Ptr(operator.UserId)

	// 更新用户
	if err = s.userRepo.Update(ctx, req); err != nil {
		s.log.Error(err)
		return nil, err
	}

	if len(req.GetPassword()) > 0 {
		if err = s.userCredentialsRepo.ResetCredential(ctx, &authenticationV1.ResetCredentialRequest{
			IdentityType:  authenticationV1.IdentityType_USERNAME,
			Identifier:    req.Data.GetUsername(),
			NewCredential: req.GetPassword(),
		}); err != nil {
			return nil, err
		}
	}

	return &emptypb.Empty{}, nil
}

func (s *UserService) Delete(ctx context.Context, req *userV1.DeleteUserRequest) (*emptypb.Empty, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return &emptypb.Empty{}, err
	}

	// 获取操作者的用户信息
	operatorUser, err := s.userRepo.Get(ctx, operator.UserId)
	if err != nil {
		return nil, err
	}

	// 校验操作者的权限
	if operatorUser.GetAuthority() != userV1.UserAuthority_SYS_ADMIN {
		return nil, adminV1.ErrorForbidden("权限不够")
	}

	// 获取将被删除的用户信息
	user, err := s.userRepo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	// 不能删除超级管理员
	if user.GetAuthority() == userV1.UserAuthority_SYS_ADMIN {
		return nil, adminV1.ErrorForbidden("闹哪样？不能删除超级管理员！")
	}

	if operatorUser.GetAuthority() == user.GetAuthority() {
		return nil, adminV1.ErrorForbidden("不能删除同级用户！")
	}

	// 删除用户
	err = s.userRepo.Delete(ctx, req.GetId())

	return &emptypb.Empty{}, err
}

func (s *UserService) UserExists(ctx context.Context, req *userV1.UserExistsRequest) (*userV1.UserExistsResponse, error) {
	return s.userRepo.UserExists(ctx, req)
}

// CreateDefaultUser 创建默认用户，即超级用户
func (s *UserService) CreateDefaultUser(ctx context.Context) error {
	const (
		defaultUsername = "admin"
		defaultPassword = "admin"
	)

	var err error

	if _, err = s.userRepo.Create(ctx, &userV1.CreateUserRequest{
		Data: &userV1.User{
			Id:        trans.Ptr(uint32(1)),
			Username:  trans.Ptr(defaultUsername),
			Realname:  trans.Ptr("大灰狼"),
			Nickname:  trans.Ptr("鹳狸猿"),
			Region:    trans.Ptr("中国"),
			Email:     trans.Ptr("admin@gmail.com"),
			Authority: userV1.UserAuthority_SYS_ADMIN.Enum(),
			Roles:     []string{"super"},
		},
	}); err != nil {
		s.log.Errorf("create default user err: %v", err)
		return err
	}

	err = s.userCredentialsRepo.Create(ctx, &authenticationV1.CreateUserCredentialRequest{
		Data: &authenticationV1.UserCredential{
			UserId:         trans.Ptr(uint32(1)),
			IdentityType:   authenticationV1.IdentityType_USERNAME.Enum(),
			Identifier:     trans.Ptr(defaultUsername),
			CredentialType: authenticationV1.CredentialType_PASSWORD_HASH.Enum(),
			Credential:     trans.Ptr(defaultPassword),
			IsPrimary:      trans.Ptr(true),
			Status:         authenticationV1.UserCredentialStatus_ENABLED.Enum(),
		},
	})
	if err != nil {
		s.log.Errorf("create default user credential err: %v", err)
		return err
	}

	return err
}
