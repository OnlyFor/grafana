import { ScopeDashboardBinding, ScopeNodeSpec } from '@grafana/data';

export interface Node extends ScopeNodeSpec {
  name: string;
  isExpandable: boolean;
  isSelectable: boolean;
  isExpanded: boolean;
  query: string;
  nodes: NodesMap;
}

export type NodesMap = Record<string, Node>;

export interface SuggestedDashboard {
  dashboard: string;
  dashboardTitle: string;
  items: ScopeDashboardBinding[];
}
