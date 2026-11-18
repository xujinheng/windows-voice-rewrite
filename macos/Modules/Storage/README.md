# VoiceRewriteStorage (macOS)

Standalone Swift Package for the native macOS host. Only this package persists
settings; the Go Helper receives request-scoped configuration.

## Build and test

From macos/Modules/Storage on macOS:

    swift build
    swift test
    # Only on a provisioned disposable macOS Keychain test identity:
    VOICE_REWRITE_TEST_REAL_KEYCHAIN=1 swift test

The opt-in integration test uses a UUID-scoped service and fixture-only accounts
and deletes both entries after the test. A mocked Security test is NOT proof of
real macOS Keychain behavior. Verify the host signing identity and Keychain
entitlements at integration time. No insecure fallback is provided.

## Native host usage

    import VoiceRewriteStorage

    let store = try SettingsStore()
    var settings = try store.load()  // absent file -> MacSettings.defaults
    settings.maxRecordSeconds = 180
    try store.save(settings)

    let keychain = KeychainCredentials()
    // On initial user setup, add credentials:
    // try keychain.add(userProvidedASRKey, for: .asr)
    // try keychain.add(userProvidedLLMKey, for: .llm)
    // On change: try keychain.update(newUserKey, for: .asr)
    // On removal: try keychain.delete(.asr)

    let factory = HelperConfigFactory(credentials: keychain)
    try factory.withEphemeralConfig(settings: settings, operation: .transcribe) { config in
        // Add config to one in-memory Go Helper IPC request.
        // Serialize only to the pipe, never disk/logs/UserDefaults.
    }

Core applies the remaining platform-neutral defaults. The SettingsStore only
accepts MacSettings and cannot persist an ephemeral Helper config dictionary.
Missing API keys are represented as empty strings for local unauthenticated providers. A transcription reads only the ASR credential; a selected-text task reads only the LLM credential. Locked, denied, corrupt or unavailable Keychain access still fails closed before invoking the Helper.

## Security boundaries

MacSettings is Codable with exactly five keys: asr_base_url, llm_base_url,
asr_model, text_model and max_record_seconds. Default recording limit: 180s.
The file is ~/Library/Application Support/VoiceRewrite/settings.json.
Directory mode is 0700 and file mode 0600. Symlinks, shared permissions,
unknown JSON fields (including secret fields), malformed JSON, and
credential-bearing base URLs are rejected. A temporary 0600 file is synced
before atomic rename into place; failed pre-rename operations preserve
the old valid settings file.

Credentials are generic passwords in Security.framework, service
com.xujinheng.VoiceRewrite, distinct accounts asr_api_key and llm_api_key.
Implementation uses SecItemAdd, SecItemCopyMatching, SecItemUpdate and
SecItemDelete, the data-protection Keychain, and
kSecAttrAccessibleWhenUnlockedThisDeviceOnly. Missing, duplicate,
locked/interaction-required, denied, unavailable and unknown OSStatus errors
are surfaced distinctly. The implementation never silently falls back to
plaintext or a fake cipher.

The ephemeral IPC config is deliberately not Codable. Only the relevant provider key is sent per operation; the other key is empty. Never print, log, persist
or embed it in crash reports. macOS host app signing and sandbox Keychain
entitlements are outside this module's file ownership and must be verified
during integration. The opt-in integration test requires real macOS access.
