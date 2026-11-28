# Fix plan: a rollback is not recognised when two revisions are one second apart or less

**Status:** plan only. No controller code has been changed. Unlike the other
plans in this directory, this one does not propose a specific change: the
options below each need a decision.

## The defect

`isRollback` in `rollout/sync.go` treats an update as a rollback to an earlier
revision when the new ReplicaSet is older than the stable one:

```go
rollbackToPreviousRevision := c.newRS.CreationTimestamp.Before(&c.stableRS.CreationTimestamp)
```

Creation timestamps have one-second resolution and `Before` is strict. If the
ReplicaSet being rolled back to and the stable ReplicaSet were created in the
same second, the comparison is false and the update is handled as a new
rollout: no fast-track, so a blue-green rollout pauses, and steps and analysis
are not skipped.

The same comparison is in `isRollbackWithinWindow`.

Outside tests this needs two revisions created within the same second, which is
rare. It is low priority.

## Evidence

`TestBlueGreenDuration_SupersededRollback` failed with `timeout after 10s
waiting for condition status=Healthy` in 7 of 20 runs of the unconverted, serial
suite with reruns off (fork workflow run 46061128211, commit `5a514fbb`,
Kubernetes 1.37; each run had an unrelated seeded bug applied).

In the run looked at closely (fork workflow run 46062202596, parallelism 4),
the ReplicaSets of revision 1 and revision 2 were both created at 20:41:33.
Revision 2 was promoted, revision 3 started, and the rollout was updated back
to revision 1 at 20:41:35, well inside the 30-second scale-down delay. The
controller log shows the pause being added again right after it removed the
scale-down deadline from the revision 1 ReplicaSet. It has no `Fast rollback or
promoted state detected` line for that ReplicaSet after the update back to it;
the only such lines are from 20:41:33, when revision 1 was first deployed.

Three other tests in `test/e2e/duration_test.go` already sleep one second
between revisions, with a comment naming this cause.

`TestBlueGreenAnalysis` reaches the same case when its Rollout turns Healthy
quickly: it updates the Rollout as soon as it is Healthy and later applies the
first manifest again. It failed in 2 of 30 runs with reruns off (fork workflow
run 46107323320, commit `9f52246a`, parallelism 8), with `Rollout status
expected to be 'Healthy'. actual: Progressing` two seconds after the rollback.
In the one looked at (job 1, namespace `e2e-bluegreenanalysis-k84vt`) the
ReplicaSets of revision 1 and revision 2 were both created at 04:47:40, and
the controller log has no `Fast rollback or promoted state detected` line for
the revision 1 ReplicaSet after the update back to it at 04:48:01. The test is
unchanged; it passes when it is rerun.

## Options

- **Break the tie with something that orders ReplicaSets within a second.** The
  revision annotation cannot be used directly: by the time `isRollback` runs,
  the ReplicaSet being rolled back to has already been given the newest
  revision number. A record of the revisions a ReplicaSet has had would do it,
  if one is kept; that was not checked.
- **Treat equal timestamps as "older" when the new ReplicaSet is not freshly
  created.** This needs a reliable sign that the ReplicaSet existed before this
  update, or a brand-new ReplicaSet created in the same second as the stable
  one would be fast-tracked past its steps and analysis, which is worse than
  the current behaviour.
- **Leave it and keep the one-second separation in the tests.** This is what
  the tests do today.

## Tests

`TestBlueGreenDuration_SupersededRollback` now has the same one-second
separation as its sibling tests, so it no longer exercises this case. A test
for a fix would create two revisions without the separation and expect the
rollback to be fast-tracked; it can only fail some of the time, because it
depends on both ReplicaSets landing in the same second.
