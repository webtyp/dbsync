# Test Suite

Summary of tests covering `webtyp.com/dbsync`.

## Test Inventory

| Test | Location | Verifies |
|---|---|---|
| `TestQueue_StoresTheOperationAsText` | `tests/typed_ops_test.go` | Offline write queues mutation storing `Operation.String()`. |
| `TestQueue_PushUsesTheStoredOperation` | `tests/typed_ops_test.go` | Push to server parses stored operation and calls the REST endpoint. |
| `TestQueue_OldFormatGoesToTheTray` | `tests/typed_ops_test.go` | Unparseable old operation format is rejected to tray without push. |
| `TestOffline_ReadNeverQueues` | `tests/typed_ops_test.go` | Local read operation answers from local database without queuing. |
| `TestHarvest_CrudWiringErrorStopsClient` | `tests/harvest_wiring_test.go` | Module wiring failure during harvest returns error and stops client. |
| `TestMigrate_UpToDate_OnlyEnsuresTables` | `tests/migrate_test.go` | Schema migration up to date ensures tables exist. |
| `TestMigrate_NewField_AddsOnlyThatColumn` | `tests/migrate_test.go` | New field in schema adds only the missing column. |
| `TestMigrate_IntrospectionFails_ReturnsErrorAndStops` | `tests/migrate_test.go` | Introspection error aborts migration. |
| `TestIndicatorShowsOnline` | `tests/ui_test.go` | UI indicator renders online state. |
| `TestIndicatorFollowsTheConnection` | `tests/ui_test.go` | Indicator transitions between online and offline. |
| `TestTrayListsRefusedWritesUntilDismissed` | `tests/ui_test.go` | Tray lists rejected writes with reasons until dismissed. |
| `TestRenderCSSStylesBothWidgets` | `tests/ui_test.go` | RenderCSS produces styles for indicator and tray. |
| `TestServer_PublishesHeadOnChange` | `tests/dbsync_test.go` | Server publishes head update when change log advances. |
| `TestServer_EventsNeedSession` | `tests/dbsync_test.go` | SSE events endpoint requires authenticated session. |
| `TestClient_OpenRequiresRecords` | `tests/dbsync_test.go` | Opening client requires records container. |
| `TestClient_PullPutsRowsInRecords` | `tests/dbsync_test.go` | Client pull saves server rows into local records. |
| `TestBootstrapCopiesOnlyTheUsersRows` | `tests/dbsync_test.go` | Initial bootstrap downloads only rows matching user scope. |
| `TestOnlineWriteIsLocalAtOnceThenOnTheServer` | `tests/dbsync_test.go` | Online write applies locally first then delivers to server. |
| `TestOfflineWriteIsDeliveredWhenTheServerComesBack` | `tests/dbsync_test.go` | Offline write queues locally and delivers upon reconnection. |
| `TestLostAnswerIsAnsweredOnce` | `tests/dbsync_test.go` | Idempotent retry answers without reapplying mutation. |
| `TestRejectedWriteIsUndoneLocallyAndListed` | `tests/dbsync_test.go` | Server rejection rolls back local write and records in tray. |
| `TestRejectedUpdateRestoresTheServersRow` | `tests/dbsync_test.go` | Server update rejection restores original server state. |
| `TestPullAppliesCreatesDeletesAndScopeChanges` | `tests/dbsync_test.go` | Pull reconciles creations, deletions, and scope adjustments. |
| `TestPullPagesPastFilteredChanges` | `tests/dbsync_test.go` | Pull paginates through intermediate changes. |
| `TestPullWaitsForPush` | `tests/dbsync_test.go` | Pull sequence waits for pending push to settle. |
| `TestEpochChangeResendsToAStandbyThatMissedIt` | `tests/dbsync_test.go` | Epoch change resends to a standby that missed it. |
| `TestSecondTabIsRefused` | `tests/dbsync_test.go` | Opening client in multiple tabs is prevented. |
| `TestFirstRunNeedsTheServer` | `tests/dbsync_test.go` | First client initialization requires reachable server. |
| `TestOnlyDeclaredOperationsRunOffline` | `tests/dbsync_test.go` | Only modules with declared operations run offline. |
| `TestUnsupportedScopeOperatorAnswers500` | `tests/dbsync_test.go` | Unsupported scope filter operator returns 500 error. |
| `TestFieldsSendOnlyTheChosenColumns` | `tests/dbsync_test.go` | Field filtering transfers only selected columns. |
| `TestUnknownFieldAnswers500` | `tests/dbsync_test.go` | Unknown field in rule returns 500 error. |
