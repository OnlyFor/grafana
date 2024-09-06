package ossaccesscontrol

import (
	"github.com/grafana/grafana/pkg/api/routing"
	"github.com/grafana/grafana/pkg/apimachinery/identity"
	"github.com/grafana/grafana/pkg/infra/db"
	"github.com/grafana/grafana/pkg/services/accesscontrol"
	"github.com/grafana/grafana/pkg/services/accesscontrol/resourcepermissions"
	"github.com/grafana/grafana/pkg/services/featuremgmt"
	"github.com/grafana/grafana/pkg/services/licensing"
	"github.com/grafana/grafana/pkg/services/ngalert"
	alertingac "github.com/grafana/grafana/pkg/services/ngalert/accesscontrol"
	"github.com/grafana/grafana/pkg/services/org"
	"github.com/grafana/grafana/pkg/services/team"
	"github.com/grafana/grafana/pkg/services/user"
	"github.com/grafana/grafana/pkg/setting"
)

var ReceiversViewActions = []string{accesscontrol.ActionAlertingReceiversRead}
var ReceiversEditActions = append(ReceiversViewActions, []string{accesscontrol.ActionAlertingReceiversUpdate, accesscontrol.ActionAlertingReceiversDelete}...)
var ReceiversAdminActions = append(ReceiversEditActions, []string{accesscontrol.ActionAlertingReceiversReadSecrets, accesscontrol.ActionAlertingReceiversPermissionsRead, accesscontrol.ActionAlertingReceiversPermissionsWrite}...)

func registerReceiverRoles(cfg *setting.Cfg, service accesscontrol.Service) error {
	if !cfg.RBAC.PermissionsWildcardSeed("receiver") { // TODO: Do we need wildcard seed support in alerting?
		return nil
	}

	viewer := accesscontrol.RoleRegistration{
		Role: accesscontrol.RoleDTO{
			Name:        "fixed:receivers:viewer",
			DisplayName: "Viewer",
			Description: "View all receivers",
			Group:       ngalert.AlertRolesGroup,
			Permissions: accesscontrol.PermissionsForActions(ReceiversViewActions, alertingac.ScopeReceiversAll),
			Hidden:      true,
		},
		Grants: []string{string(org.RoleViewer)},
	}

	editor := accesscontrol.RoleRegistration{
		Role: accesscontrol.RoleDTO{
			Name:        "fixed:receivers:editor",
			DisplayName: "Editor",
			Description: "Edit all receivers.",
			Group:       ngalert.AlertRolesGroup,
			Permissions: accesscontrol.PermissionsForActions(ReceiversEditActions, alertingac.ScopeReceiversAll),
			Hidden:      true,
		},
		Grants: []string{string(org.RoleEditor)},
	}

	admin := accesscontrol.RoleRegistration{
		Role: accesscontrol.RoleDTO{
			Name:        "fixed:receivers:admin",
			DisplayName: "Admin",
			Description: "Administer all receivers (reads secrets).",
			Group:       ngalert.AlertRolesGroup,
			Permissions: accesscontrol.PermissionsForActions(ReceiversAdminActions, alertingac.ScopeReceiversAll),
			Hidden:      true,
		},
		Grants: []string{string(org.RoleAdmin)},
	}

	return service.DeclareFixedRoles(viewer, editor, admin)
}

func ProvideReceiverPermissionsService(
	cfg *setting.Cfg, features featuremgmt.FeatureToggles, router routing.RouteRegister, sql db.DB, ac accesscontrol.AccessControl,
	license licensing.Licensing, service accesscontrol.Service,
	teamService team.Service, userService user.Service, actionSetService resourcepermissions.ActionSetService,
) (*ReceiverPermissionsService, error) {
	if !features.IsEnabledGlobally(featuremgmt.FlagAlertingApiServer) {
		return nil, nil
	}
	if err := registerReceiverRoles(cfg, service); err != nil {
		return nil, err
	}

	options := resourcepermissions.Options{
		Resource:          "receivers",
		ResourceAttribute: "uid",
		Assignments: resourcepermissions.Assignments{
			Users:           true,
			Teams:           true,
			BuiltInRoles:    true,
			ServiceAccounts: true,
		},
		PermissionsToActions: map[string][]string{
			string(alertingac.ReceiverPermissionView):  append([]string{}, ReceiversViewActions...),
			string(alertingac.ReceiverPermissionEdit):  append([]string{}, ReceiversEditActions...),
			string(alertingac.ReceiverPermissionAdmin): append([]string{}, ReceiversAdminActions...),
		},
		ReaderRoleName: "Alerting receiver permission reader",
		WriterRoleName: "Alerting receiver permission writer",
		RoleGroup:      ngalert.AlertRolesGroup,
	}

	srv, err := resourcepermissions.New(cfg, options, features, router, license, ac, service, sql, teamService, userService, actionSetService)
	if err != nil {
		return nil, err
	}
	return &ReceiverPermissionsService{srv, service}, nil
}

func (r ReceiverPermissionsService) ClearUserPermissionCache(user identity.Requester) {
	r.ac.ClearUserPermissionCache(user)
}

var _ accesscontrol.ReceiverPermissionsService = new(ReceiverPermissionsService)

type ReceiverPermissionsService struct {
	*resourcepermissions.Service
	ac accesscontrol.Service
}