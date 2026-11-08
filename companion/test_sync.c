#define main sf_launcher_main
#define wmain sf_launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
#include <assert.h>
static void save_json(const char *dir, const char *name, cJSON *json) { assert(!desktop_json(dir, name, json)); }
static void write_text(const char *path, const char *text) { assert(!capture_atomic(path, text, strlen(text))); }
static void acknowledge(CaptureCollector *collector, const char *source, const char *url) {
    size_t n = 0; char *text = capture_read(source, &n); char hash[65], name[80], *ack = malloc(PATH_CAP);
    assert(text && ack && !capture_hash_bytes(text, n, hash)); free(text);
    snprintf(name, sizeof(name), "%s.json", hash); assert(!capture_ack_path(collector, name, url, ack)); write_text(ack, "acknowledged\n"); free(ack);
}
int main(int argc, char **argv) {
    _set_error_mode(_OUT_TO_STDERR); assert(argc == 2);
    char (*path)[PATH_CAP] = calloc(4, sizeof(*path)); assert(path && !mkdir_tree(argv[1]));
    cJSON *cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "apiBase", "https://example.invalid"); cJSON_AddStringToObject(cfg, "game", "forever");
    cJSON_AddBoolToObject(cfg, "consentGranted", 1); cJSON *watch = cJSON_AddArrayToObject(cfg, "watch"), *logs = cJSON_AddArrayToObject(cfg, "watchLogs");
    save_json(argv[1], "config.json", cfg); SyncCache cache = {0}; assert(sync_state(&cache, argv[1]) == SF_SYNC_READY);
    cJSON_ReplaceItemInObjectCaseSensitive(cfg, "consentGranted", cJSON_CreateBool(0)); save_json(argv[1], "config.json", cfg);
    assert(sync_state(&cache, argv[1]) == SF_SYNC_PAUSED);
    cJSON_ReplaceItemInObjectCaseSensitive(cfg, "consentGranted", cJSON_CreateBool(1));
    char url[4096]; assert(!capture_url(cfg, url));
    cJSON *permission = cJSON_CreateObject(); cJSON_AddStringToObject(permission, "endpoint", url); cJSON_AddBoolToObject(permission, "approved", 1); save_json(argv[1], "capture-consent.json", permission);
    assert(!addon_join(path[0], argv[1], "SimforeverProbe.lua")); write_text(path[0], "SimforeverProbeDB={version='0.35.0'}");
    cJSON_AddItemToArray(watch, cJSON_CreateString(path[0])); save_json(argv[1], "config.json", cfg);
    CaptureCollector *collector = calloc(1, sizeof(*collector)); assert(collector && !addon_join(collector->queue, argv[1], "recordings") && !mkdir_tree(collector->queue));
    uint64_t used = 0; assert(!capture_enqueue(collector, path[0], &used));
    assert(sync_state(&cache, argv[1]) == SF_SYNC_PENDING); acknowledge(collector, path[0], url);
    assert(sync_state(&cache, argv[1]) == SF_SYNC_DONE);
    write_text(path[0], "SimforeverProbeDB={version='0.35.0',newReading=3}");
    assert(sync_state(&cache, argv[1]) == SF_SYNC_PENDING);
    assert(!capture_enqueue(collector, path[0], &used)); acknowledge(collector, path[0], url); assert(sync_state(&cache, argv[1]) == SF_SYNC_DONE);
    assert(!addon_join(path[1], argv[1], "WoWCombatLog.txt")); write_text(path[1], "1/1 12:00:00.000  SWING_DAMAGE,1,2,3\n");
    cJSON_AddItemToArray(logs, cJSON_CreateString(path[1])); save_json(argv[1], "config.json", cfg); assert(sync_state(&cache, argv[1]) == SF_SYNC_PENDING);
    cJSON *state = cJSON_CreateObject(), *entries = cJSON_AddObjectToObject(state, "entries");
    char *key = malloc(strlen(path[1]) + 5); assert(key); strcpy(key, "log:"); strcat(key, path[1]);
    cJSON *entry = cJSON_AddObjectToObject(entries, key); cJSON_AddStringToObject(entry, "uploadedAt", "2099-01-01T00:00:00Z"); cJSON_AddStringToObject(entry, "hash", sync_hash(&cache, path[1])); save_json(argv[1], "state.json", state);
    assert(sync_state(&cache, argv[1]) == SF_SYNC_DONE);
    cJSON_ReplaceItemInObjectCaseSensitive(entry, "uploadedAt", cJSON_CreateString("2000-01-01T00:00:00Z")); save_json(argv[1], "state.json", state);
    write_text(path[1], "1/1 12:00:00.000  SWING_DAMAGE,1,2,3\n1/1 12:00:01.000  SWING_DAMAGE,4,5,6\n");
    assert(sync_state(&cache, argv[1]) == SF_SYNC_PENDING);
    for (SyncHash *cached = cache.files; cached; cached = cached->next)
        if (!strcmp(cached->path, path[1])) assert(!strcmp(cached->hash, capture_text(entry, "hash")));
    cJSON_ReplaceItemInObjectCaseSensitive(entry, "uploadedAt", cJSON_CreateString("2099-01-01T00:00:00Z"));
    cJSON_ReplaceItemInObjectCaseSensitive(entry, "hash", cJSON_CreateString(sync_hash(&cache, path[1]))); save_json(argv[1], "state.json", state); assert(sync_state(&cache, argv[1]) == SF_SYNC_DONE);
    assert(!addon_join(path[2], collector->queue, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.lua")); write_text(path[2], "unsupported recording");
    assert(sync_state(&cache, argv[1]) == SF_SYNC_PENDING); remove_file(path[2]); assert(sync_state(&cache, argv[1]) == SF_SYNC_DONE);
    cJSON_ReplaceItemInObjectCaseSensitive(cfg, "apiBase", cJSON_CreateString("https://other.invalid")); save_json(argv[1], "config.json", cfg); assert(sync_state(&cache, argv[1]) == SF_SYNC_PENDING);
    /* A session log the game is still writing is a recording, not an upload;
     * anything else pending keeps the window on Syncing. */
    assert(sync_with_logs(SF_SYNC_DONE, 1, 1) == SF_SYNC_WAITING && sync_with_logs(SF_SYNC_READY, 2, 2) == SF_SYNC_WAITING);
    assert(sync_with_logs(SF_SYNC_DONE, 2, 1) == SF_SYNC_PENDING && sync_with_logs(SF_SYNC_PENDING, 1, 1) == SF_SYNC_PENDING);
    assert(sync_with_logs(SF_SYNC_READY, 0, 0) == SF_SYNC_DONE && sync_with_logs(SF_SYNC_PAUSED, 0, 0) == SF_SYNC_PAUSED);
    assert(sync_with_logs(SF_SYNC_READY, -1, 0) == SF_SYNC_READY);
    assert(sync_uploaded_at("2026-11-02T15:00:00Z") == sync_uploaded_at("2026-11-02T17:00:00+02:00"));
    assert(!sync_uploaded_at("2026-11-02T15:00:NaNZ") && !sync_uploaded_at("invalid"));
    sync_cache_free(&cache); cJSON_Delete(cfg); cJSON_Delete(permission); cJSON_Delete(state); free(key); free(collector); free(path);
    puts("sync status checks passed: empty, paused, pending evidence, exact acknowledgements, changed sources/logs, raw archives, endpoint changes, a session still recording and UTC offsets"); return 0;
}
