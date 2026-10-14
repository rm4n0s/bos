# Acceptance tests

Each acceptance test of [SPEC.md](SPEC.md) (§13 and §20) and the tests that run it. A test named here must exist; `internal/acceptance` checks that every acceptance number has at least one and that every name is a real test function. The tests that begin `TestAcceptance` run the whole program or the whole application core; the others check the same behaviour closer to the code.

Run them all with `go test -race ./...`.

| # | Behaviour | Tests |
|---|---|---|
| 1 | Selections, include and exclude, fixers | `TestParseStructure`, `TestParseValid` |
| 1a | Valid and invalid tables of §6.12 | `TestParseValid`, `TestParseInvalid`, `TestExpandValidTable`, `TestValidateSemanticRows` |
| 1b | Spacing and the JSON round trip | `TestParseSpacingEquivalence`, `TestJSONRoundTripValidCommands` |
| 1c | Job order is deterministic | `TestExpandOrder` |
| 2 | Merge keeps duplicates, zip pairs | `TestExpandMergeKeepsDuplicates`, `TestExpandZip`, `TestWhatAnItemCarriesReachesTheJob` |
| 3 | 3 × 4 × 2 gives 24 jobs in order | `TestExpandOrder` |
| 4 | Every unknown name is reported; nothing is created | `TestAcceptance04UnknownNamesAreEachReportedAndNothingIsCreated`, `TestValidateCollectsEveryProblem`, `TestInvalidCommandStoresNothing` |
| 5 | Region keywords and two region groups | `TestValidateRegionDiffMessages`, `TestParseInvalid` |
| 6 | Random seeds differ | `TestRandomSeedGeneratorsGetAFreshSeedPerJob`, `TestSeedsDifferBetweenJobs` |
| 7 | LoRA injection, both workflow styles | `TestInjectLoRAsCheckpointStyle`, `TestInjectLoRAsSplitStyle` |
| 8 | A job with two fixers | `TestFixerStepsRunInOrderEachOnItsServer`, `TestAcceptanceWholeProgram` |
| 9 | A failure halts the queued jobs | `TestAWorkflowFailureHaltsTheBatch` |
| 10 | Download of a command with no finished job, of one that is only partly finished, and of a finished one | `TestDownloadRefusals`, `TestDownloadArchivesTheResults`, `TestDownloadOfACommandThatIsNotFinished`, `TestAcceptanceWholeProgram` |
| 11 | Templates | `TestExpandTemplates`, `TestRemovePlaceholders` |
| 12 | Deletion rules | `TestDeleteGroupCascades`, `TestDeleteGroupRemovesItsFiles`, `TestDeleteProjectCascades`, `TestDeleteCommandAndProject`, `TestUpdateRemovesFilesTheItemNoLongerUses` |
| 13 | Events come in order, across servers | `TestConcurrentAppendsAreOrdered`, `TestAfterReturnsWhatCameAfter`, `TestAcceptanceWholeProgram` |
| 14 | The tables of §6.12 | `TestParseValid`, `TestParseInvalid`, `TestExpandValidTable`, `TestValidateSemanticRows` |
| 15 | Selectors | `TestExpandSelectors`, `TestMatchPattern` |
| 16 | Literals | `TestExpandLiterals` |
| 17 | `where` | `TestExpandWhere` |
| 18 | `limit`, `sample`, `repeat` | `TestExpandLimitSampleRepeat`, `TestRecreateReproducesASample`, `TestEnsureSampleSeed` |
| 19 | Seeds with `repeat` | `TestExpandSeeds`, `TestValidateRepeatNeedsSeed` |
| 20 | `with` overrides | `TestEffectiveOutput`, `TestValidateSettings` |
| 21 | Spread and the servers' capacity | `TestExpandSpread`, `TestPropertySpread`, `TestServersWorkInParallelAndTakeTheNextJob`, `TestServerCapacityIsRespected` |
| 22 | Failover | `TestEndToEndServerLoss`, `TestAJobWhoseServerIsLostIsTriedAgain`, `TestAfterTooManyAttemptsAJobFailsButTheCommandCarriesOn`, `TestPinnedJobsNeverMove`, `TestAFailureDoesNotInterruptJobsOnOtherServers` |
| 23 | A fixer step on another server | `TestFixerStepsRunInOrderEachOnItsServer`, `TestAttachmentsAreUploadedAndInjected` |
| 24 | Preview before add | `TestPreviewCountsAndStoresNothing`, `TestPreviewWarnsAboutServers`, `TestAcceptanceWholeProgram` |
| 25 | A command ends with its last job | `TestCommandStatusOf`, `TestRunsACommandToTheEnd`, `TestAcceptanceWholeProgram` |
| 26 | Every route and its errors | `TestEveryRoute`, `TestErrorEnvelope`, `TestOpenAPIDescribesEveryRoute` |
| 27 | Preview and add disagree about invalid text | `TestPreviewAndCreateDisagree` |
| 28 | Stop mid-batch, start again | `TestShutdownLeavesRunningJobsForTheNextStart`, `TestAcceptanceRestartMidBatch`, `TestServeServesTheAPI` |
| 29 | Two browsers, and a reconnect | `TestCommandStreamUpdatesTheRowsAndTheHeader`, `TestCommandStreamResumes`, `TestEventStreamResumes` |
| 30 | Forgery check | `TestCSRF`, `TestEveryStateChangingRouteChecksForgery` |
| 31 | Hostile text is inert | `TestHostileTextIsInertEverywhere`, `TestHostileTextIsInert` |
| 32 | Duplicate titles | `TestAcceptance32DuplicateTitlesAreRefused`, `TestCheckTitlesRejectsDuplicates` |
| 33 | One winner for a job | `TestClaimHasOneWinner`, `TestConcurrentWorkersDrainTheQueue` |
| 34 | A dropped connection | `TestReconnectsWithANewClient`, `TestPromptLost`, `TestRecoveryResumesWhatWasSubmittedAndQueuesTheRest` |
| 35 | Node errors | `TestQueuePromptNodeErrors`, `TestQueueAndWarningsAreRecorded` |
| 36 | Reference images upload once | `TestUploadIsDeduplicatedByContent`, `TestAttachmentsAreUploadedAndInjected` |
| 37 | Civitai errors and the cache | `TestDownloadFailures`, `TestDownload`, `TestImportGroup` |
| 38 | Tags are lower-cased and matched | `TestAcceptance38TagsAreLowerCasedAndMatchedWhateverTheCase`, `TestTagListAndRename` |
| 39 | Tag lists per scope | `TestTagListIsSeparatePerScope`, `TestTagListAndRename` |
| 40 | Renaming merges and lists the commands | `TestTagRename`, `TestTagPanel` |
| 41 | Group tags | `TestAcceptance41GroupTagsAreNotSelectable`, `TestGroupScreen` |

What cannot be run here is a **real ComfyUI** and a **real Civitai account**. Everything above uses the stand-in ComfyUI (`internal/comfy/comfytest`) and a stand-in of Civitai's HTTP API. Before relying on a new installation, run one small command against your ComfyUI (see the tutorial).
