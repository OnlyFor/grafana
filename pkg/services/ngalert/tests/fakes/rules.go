package fakes

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/grafana/grafana/pkg/infra/metrics"
	"github.com/grafana/grafana/pkg/services/auth/identity"
	"github.com/grafana/grafana/pkg/services/folder"
	"github.com/grafana/grafana/pkg/services/ngalert/models"
	"github.com/grafana/grafana/pkg/util"
)

// FakeRuleStore mocks the RuleStore of the scheduler.
type RuleStore struct {
	t   *testing.T
	mtx sync.Mutex
	// OrgID -> RuleGroup -> Namespace -> Rules
	Rules       map[int64][]*models.AlertRule
	Hook        func(cmd any) error // use Hook if you need to intercept some query and return an error
	RecordedOps []any
	Folders     map[int64][]*folder.Folder
}

type GenericRecordedQuery struct {
	Name   string
	Params []any
}

func NewRuleStore(t *testing.T) *RuleStore {
	return &RuleStore{
		t:     t,
		Rules: map[int64][]*models.AlertRule{},
		Hook: func(any) error {
			return nil
		},
		Folders: map[int64][]*folder.Folder{},
	}
}

// PutRule puts the rule in the Rules map. If there are existing rule in the same namespace, they will be overwritten
func (f *RuleStore) PutRule(_ context.Context, rules ...*models.AlertRule) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
mainloop:
	for _, r := range rules {
		rgs := f.Rules[r.OrgID]
		for idx, rulePtr := range rgs {
			if rulePtr.UID == r.UID {
				rgs[idx] = r
				continue mainloop
			}
		}
		rgs = append(rgs, r)
		f.Rules[r.OrgID] = rgs

		var existing *folder.Folder
		folders := f.Folders[r.OrgID]
		for _, folder := range folders {
			if folder.UID == r.NamespaceUID {
				existing = folder
				break
			}
		}
		if existing == nil {
			metrics.MFolderIDsServiceCount.WithLabelValues(metrics.NGAlerts).Inc()
			title := "TEST-FOLDER-" + util.GenerateShortUID()
			folders = append(folders, &folder.Folder{
				ID:       rand.Int63(), // nolint:staticcheck
				UID:      r.NamespaceUID,
				Title:    title,
				Fullpath: "fullpath_" + title,
			})
			f.Folders[r.OrgID] = folders
		}
	}
}

// GetRecordedCommands filters recorded commands using predicate function. Returns the subset of the recorded commands that meet the predicate
func (f *RuleStore) GetRecordedCommands(predicate func(cmd any) (any, bool)) []any {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	result := make([]any, 0, len(f.RecordedOps))
	for _, op := range f.RecordedOps {
		cmd, ok := predicate(op)
		if !ok {
			continue
		}
		result = append(result, cmd)
	}
	return result
}

func (f *RuleStore) DeleteAlertRulesByUID(_ context.Context, orgID int64, UIDs ...string) error {
	f.RecordedOps = append(f.RecordedOps, GenericRecordedQuery{
		Name:   "DeleteAlertRulesByUID",
		Params: []any{orgID, UIDs},
	})

	rules := f.Rules[orgID]

	var result = make([]*models.AlertRule, 0, len(rules))

	for _, rule := range rules {
		add := true
		for _, UID := range UIDs {
			if rule.UID == UID {
				add = false
				break
			}
		}
		if add {
			result = append(result, rule)
		}
	}

	f.Rules[orgID] = result
	return nil
}

func (f *RuleStore) GetAlertRuleByUID(_ context.Context, q *models.GetAlertRuleByUIDQuery) (*models.AlertRule, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.RecordedOps = append(f.RecordedOps, *q)
	if err := f.Hook(*q); err != nil {
		return nil, err
	}
	rules, ok := f.Rules[q.OrgID]
	if !ok {
		return nil, nil
	}

	for _, rule := range rules {
		if rule.UID == q.UID {
			return rule, nil
		}
	}
	return nil, models.ErrAlertRuleNotFound
}

func (f *RuleStore) GetAlertRulesGroupByRuleUID(_ context.Context, q *models.GetAlertRulesGroupByRuleUIDQuery) ([]*models.AlertRule, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.RecordedOps = append(f.RecordedOps, *q)
	if err := f.Hook(*q); err != nil {
		return nil, err
	}
	rules, ok := f.Rules[q.OrgID]
	if !ok {
		return nil, nil
	}

	var selected *models.AlertRule
	for _, rule := range rules {
		if rule.UID == q.UID {
			selected = rule
			break
		}
	}
	if selected == nil {
		return nil, nil
	}

	ruleList := []*models.AlertRule{}
	for _, rule := range rules {
		if rule.GetGroupKey() == selected.GetGroupKey() {
			ruleList = append(ruleList, rule)
		}
	}
	return ruleList, nil
}

func (f *RuleStore) ListAlertRules(_ context.Context, q *models.ListAlertRulesQuery) (models.RulesGroup, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.RecordedOps = append(f.RecordedOps, *q)

	if err := f.Hook(*q); err != nil {
		return nil, err
	}

	hasDashboard := func(r *models.AlertRule) bool {
		if q.DashboardUID != "" {
			if r.DashboardUID == nil || *r.DashboardUID != q.DashboardUID {
				return false
			}
			if q.PanelID > 0 {
				if r.PanelID == nil || *r.PanelID != q.PanelID {
					return false
				}
			}
		}
		return true
	}

	fieldIn := func(field func(r *models.AlertRule) string, in []string) func(r *models.AlertRule) bool {
		return func(r *models.AlertRule) bool {
			if len(in) > 0 {
				for _, v := range in {
					if field(r) == v {
						return true
					}
				}
				return false
			}
			return true
		}
	}

	hasNamespace := fieldIn(func(r *models.AlertRule) string { return r.NamespaceUID }, q.NamespaceUIDs)
	hasRuleUID := fieldIn(func(r *models.AlertRule) string { return r.UID }, q.RuleUIDs)

	ruleList := models.RulesGroup{}
	for _, r := range f.Rules[q.OrgID] {
		if !hasDashboard(r) {
			continue
		}
		if !hasNamespace(r) {
			continue
		}
		if !hasRuleUID(r) {
			continue
		}
		if q.RuleGroup != "" && r.RuleGroup != q.RuleGroup {
			continue
		}
		ruleList = append(ruleList, r)
	}

	return ruleList, nil
}

func (f *RuleStore) GetUserVisibleNamespaces(_ context.Context, orgID int64, _ identity.Requester) (map[string]*folder.Folder, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	namespacesMap := map[string]*folder.Folder{}

	_, ok := f.Rules[orgID]
	if !ok {
		return namespacesMap, nil
	}

	for _, folder := range f.Folders[orgID] {
		namespacesMap[folder.UID] = folder
	}
	return namespacesMap, nil
}

func (f *RuleStore) GetNamespaceByUID(_ context.Context, uid string, orgID int64, user identity.Requester) (*folder.Folder, error) {
	q := GenericRecordedQuery{
		Name:   "GetNamespaceByUID",
		Params: []any{orgID, uid, user},
	}
	defer func() {
		f.RecordedOps = append(f.RecordedOps, q)
	}()
	err := f.Hook(q)
	if err != nil {
		return nil, err
	}
	folders := f.Folders[orgID]
	for _, folder := range folders {
		if folder.UID == uid {
			return folder, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *RuleStore) UpdateAlertRules(_ context.Context, q []models.UpdateRule) error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.RecordedOps = append(f.RecordedOps, q)
	if err := f.Hook(q); err != nil {
		return err
	}
	return nil
}

func (f *RuleStore) InsertAlertRules(_ context.Context, q []models.AlertRule) ([]models.AlertRuleKeyWithId, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.RecordedOps = append(f.RecordedOps, q)
	ids := make([]models.AlertRuleKeyWithId, 0, len(q))
	for _, rule := range q {
		ids = append(ids, models.AlertRuleKeyWithId{
			AlertRuleKey: rule.GetKey(),
			ID:           rand.Int63(),
		})
	}
	if err := f.Hook(q); err != nil {
		return ids, err
	}
	return ids, nil
}

func (f *RuleStore) InTransaction(ctx context.Context, fn func(c context.Context) error) error {
	return fn(ctx)
}

func (f *RuleStore) GetRuleGroupInterval(ctx context.Context, orgID int64, namespaceUID string, ruleGroup string) (int64, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.RecordedOps = append(f.RecordedOps, GenericRecordedQuery{
		Name:   "GetRuleGroupInterval",
		Params: []any{orgID, namespaceUID, ruleGroup},
	})
	for _, rule := range f.Rules[orgID] {
		if rule.RuleGroup == ruleGroup && rule.NamespaceUID == namespaceUID {
			return rule.IntervalSeconds, nil
		}
	}
	return 0, models.ErrAlertRuleGroupNotFound.Errorf("")
}

func (f *RuleStore) UpdateRuleGroup(ctx context.Context, orgID int64, namespaceUID string, ruleGroup string, interval int64) error {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	for _, rule := range f.Rules[orgID] {
		if rule.RuleGroup == ruleGroup && rule.NamespaceUID == namespaceUID {
			rule.IntervalSeconds = interval
		}
	}
	return nil
}

func (f *RuleStore) IncreaseVersionForAllRulesInNamespace(_ context.Context, orgID int64, namespaceUID string) ([]models.AlertRuleKeyWithVersion, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	f.RecordedOps = append(f.RecordedOps, GenericRecordedQuery{
		Name:   "IncreaseVersionForAllRulesInNamespace",
		Params: []any{orgID, namespaceUID},
	})

	var result []models.AlertRuleKeyWithVersion

	for _, rule := range f.Rules[orgID] {
		if rule.NamespaceUID == namespaceUID && rule.OrgID == orgID {
			rule.Version++
			rule.Updated = time.Now()
			result = append(result, models.AlertRuleKeyWithVersion{
				Version:      rule.Version,
				AlertRuleKey: rule.GetKey(),
			})
		}
	}
	return result, nil
}

func (f *RuleStore) CountInFolders(ctx context.Context, orgID int64, folderUIDs []string, u identity.Requester) (int64, error) {
	return 0, nil
}

func (f *RuleStore) GetNamespacesByRuleUID(ctx context.Context, orgID int64, uids ...string) (map[string]string, error) {
	f.mtx.Lock()
	defer f.mtx.Unlock()

	namespacesMap := make(map[string]string)

	rules, ok := f.Rules[orgID]
	if !ok {
		return namespacesMap, nil
	}

	uidFilter := make(map[string]struct{}, len(uids))
	for _, uid := range uids {
		uidFilter[uid] = struct{}{}
	}

	for _, rule := range rules {
		if _, ok := uidFilter[rule.UID]; ok {
			namespacesMap[rule.UID] = rule.NamespaceUID
		}
	}

	return namespacesMap, nil
}
