import { css } from '@emotion/css';
import React from 'react';

import { GrafanaTheme2 } from '@grafana/data/';
import { Button, useStyles2 } from '@grafana/ui';
import { PAGE_SIZE } from 'app/features/browse-dashboards/api/services';

import appEvents from '../../../core/app_events';
import { Trans } from '../../../core/internationalization';
import { useDispatch } from '../../../types';
import { ShowModalReactEvent } from '../../../types/events';
import { useRestoreDashboardMutation } from '../../browse-dashboards/api/browseDashboardsAPI';
import { refetchChildren, setAllSelection, useActionSelectionState } from '../../browse-dashboards/state';
import { RestoreModal } from '../components/RestoreModal';
import { useRecentlyDeletedStateManager } from '../utils/useRecentlyDeletedStateManager';

export function RecentlyDeletedActions() {
  const styles = useStyles2(getStyles);

  const dispatch = useDispatch();
  const selectedItems = useActionSelectionState();
  const [, stateManager] = useRecentlyDeletedStateManager();

  const [restoreDashboard, { isLoading: isRestoreLoading }] = useRestoreDashboardMutation();

  const onActionComplete = () => {
    dispatch(setAllSelection({ isSelected: false, folderUID: undefined }));

    stateManager.doSearchWithDebounce();
  };

  const onRestore = async () => {
    const resultsView = stateManager.state.result?.view.toArray();
    if (!resultsView) {
      return;
    }

    const uids = Object.entries(selectedItems.dashboard)
      .filter(([_, selected]) => selected)
      .map((v) => v[0]);

    const promises = uids.map((uid) => {
      return restoreDashboard({ dashboardUID: uid });
    });

    await Promise.all(promises);

    const parentUIDs = uids.map((uid) => resultsView.find((v) => v.uid === uid)?.location);
    const refreshedParents = new Set<string | undefined>();

    for (const parentUID of parentUIDs) {
      if (refreshedParents.has(parentUID)) {
        continue;
      }

      refreshedParents.add(parentUID);
      dispatch(refetchChildren({ parentUID, pageSize: PAGE_SIZE }));
    }

    onActionComplete();
  };

  const showRestoreModal = () => {
    appEvents.publish(
      new ShowModalReactEvent({
        component: RestoreModal,
        props: {
          selectedItems,
          onConfirm: onRestore,
          isLoading: isRestoreLoading,
        },
      })
    );
  };

  return (
    <div className={styles.row}>
      <Button onClick={showRestoreModal} variant="secondary">
        <Trans i18nKey="recently-deleted.buttons.restore">Restore</Trans>
      </Button>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  row: css({
    marginBottom: theme.spacing(2),
  }),
});
