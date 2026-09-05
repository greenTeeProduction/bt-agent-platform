# Remaining review fixes: deployment compatibility

This release intentionally removes side effects from GET, HEAD and OPTIONS. Legacy query-string mutation requests now return 405 and must be migrated to POST with an application/json body. No GET compatibility shim is provided: retaining side effects on safe methods would reintroduce the CSRF defect.

Affected operations include agent run, task create/approve/reject, workflow approve/reject/run-full-pipeline, thinktank analysis, chat and sprint execution. Consult the updated OpenAPI request schemas; in-repository browser and operational clients have been migrated. Cookie-authenticated mutations require X-CSRF-Token; requests authenticated by a valid platform X-API-Key are exempt from CSRF. A bare or invalid key header is not an exemption. CORS preflight permits X-CSRF-Token and Idempotency-Key. Retry non-idempotent mutations only when an explicit idempotency contract exists. Sprint retries can reuse Idempotency-Key.

Persistent blackboard scope filenames now use reversible v2 base64 identifiers. Unambiguous legacy filenames remain readable and migrate on write. Ambiguous legacy names (including underscores, previously also used for other characters) produce an explicit migration error and are never silently assigned to a guessed original scope. Preserve backups and resolve ownership explicitly before migration. No bulk destructive rename or automatic guess is performed.

Parallel commands retain child state while Running, merge outputs deterministically, and cancel and join sibling work before returning on any/race outcomes. Custom commands must honor context cancellation. WorkerPool shutdown drains accepted tasks; custom tasks must finish for shutdown to finish. Gardener reorder-only changes now pass the same final acceptance gate as other mutations.

The earlier security migration still applies: configured API credentials are required for privileged operations; remote listeners need explicit bind settings. Preserve encrypted/trusted transport. Audit/config/rules diagnostics now also require authentication.
