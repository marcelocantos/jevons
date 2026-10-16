# Target: Keychain biometry / user-presence ACL

## Why Keychain today

`jevonsd` loads pay-as-you-go API keys (`openai-api-key`, `xai-api-key`, account
`jevons`) via `/usr/bin/security find-generic-password`, and
`--set-openai-key` / `--set-xai-key` write with `security add-generic-password`
(no `-T` ACL). Plan OAuth credentials are owned by Claudia, not jevonsd.

## Prompt friction

Writes/reads go through the `security` CLI, so ACL prompts attach to that tool
and ask for the **login password**. Daemon start and key rotation are the
hotspots. No SecAccessControl / biometry flags.

## Recommended change

1. Replace `security` exec with in-process SecItem* (aligned with spyder /
   sentinel / mcpsafe).
2. On store, set `kSecAttrAccessControl` via
   `SecAccessControlCreateWithFlags(..., kSecAccessControlUserPresence)` and
   trust the jevonsd binary path/signature.
3. Prefer biometry for interactive set/re-approve; allow a documented
   fail-closed or once-per-session behavior for daemon boot if prompts would
   hang launchd.
4. Binary upgrades re-trigger Keychain ACL approval until a stable Canticode
   codesign identity exists — expected.

## Non-goals

Do not move Claudia `claudia-plan-credentials` into jevonsd. Do not put secrets
in argv (`-w`).
