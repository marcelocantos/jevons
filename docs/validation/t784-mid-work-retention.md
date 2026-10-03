# T784: mid-work reports retain the seat

A typed finish-report can close a slice while the mission continues. An explicit
in-progress status, checkpoint, or declared next/remaining work now vetoes the
finished-work reap and sends the existing outstanding-scope notice to the parent.
The same seat continues; the daemon no longer reaps it and requests a respawn.
Terminal reports with no remaining scope keep T165 behavior. T972's owned dirty
worktree, pending-gate retention, and distinct reap_done stop_reason still apply.

Journey exception: hermetic event-sink tests inject the exact terminal agent
event into the production reap path, inspect the real registry, and check parent
delivery. A provider-generated paraphrase adds nondeterminism without exercising
a different classifier. T784 status-phase fixtures preserve the earlier incident
shape; t784_mid_work_test.go labels its added T765-style fixtures as synthetic.
This exception covers classification, registry retention, and notice routing;
it does not constitute observation of the activated development binary.
