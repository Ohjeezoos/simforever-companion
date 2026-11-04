#ifdef _WIN32
#define wmain sf_launcher_wmain
#else
#define main sf_launcher_main
#endif
#include "launcher.c"
#ifdef _WIN32
#undef wmain
#else
#undef main
#endif
#include <assert.h>

static const char sample[] =
    "SimforeverProbeDB = {\r\n"
    "[\"version\"] = \"0.33.0\", [\"build\"] = {[\"version\"]=\"1.60.1\",[\"build\"]=\"70245\",[\"toc\"]=16001},\n"
    "[\"character\"] = {[\"name\"]=\"Fixture\",[\"guid\"]=\"Player-0-TEST\",[\"level\"]=30},\n"
    "[\"snapshots\"] = {[1] = {[\"id\"]=602,[\"build\"]={[\"build\"]=\"70245\"},[\"stats\"]={[\"intellectPos\"]=12}}},\n"
    "[\"people\"]={[\"peer\"]={[\"race\"]=\"Orc\"}}, [\"loot\"]={[\"source\"]={[\"npc\"]=5}},\n"
    "[\"pulls\"]={{[\"at\"]=10}},[\"history\"]={[\"old\"]={[\"build\"]={[\"build\"]=\"70235\"}}},\n"
    "[\"unknownFutureField\"]={[\"text\"]=\"line\\n\\195\\169\\\"\\\\\",[\"tiny\"]=1.25e-6},\n"
    "[\"clientApi\"]={[\"issecretvalue\"]=true,[\"IsSecretValue\"]=false},\n"
    "}\r\n";
static int attempts, fail_upload;
static char posted_hash[65];
static int fake_post(const char *url, const char *json, long *status) {
    assert(strstr(url, "/v1/games/forever/probe"));
    cJSON *root = cJSON_Parse(json); assert(root);
    cJSON *record = cJSON_GetObjectItemCaseSensitive(root, "capture"); assert(cJSON_IsObject(record));
    assert(cJSON_GetObjectItemCaseSensitive(record, "unknownFutureField"));
    assert(!cJSON_HasObjectItem(root, "ratings") && !cJSON_HasObjectItem(root, "events"));
    const char *hash = capture_text(cJSON_GetObjectItemCaseSensitive(root, "evidence"), "sha256");
    assert(hash && strlen(hash) == 64); strcpy(posted_hash, hash);
    cJSON_Delete(root); ++attempts; *status = fail_upload ? 503 : 201; return fail_upload ? -1 : 0;
}
static void write_config(const char *dir, const char *source, int consent, const char *api) {
    cJSON *root = cJSON_CreateObject(), *paths = cJSON_CreateArray();
    cJSON_AddBoolToObject(root, "consentGranted", consent);
    cJSON_AddStringToObject(root, "apiBase", api);
    cJSON_AddStringToObject(root, "game", "forever");
    cJSON_AddItemToArray(paths, cJSON_CreateString(source));
    cJSON_AddItemToObject(root, "watch", paths);
    char *json = cJSON_PrintUnformatted(root); char path[PATH_CAP]; assert(!path_join(path, dir, "config.json"));
    assert(!capture_atomic(path, json, strlen(json))); free(json); cJSON_Delete(root);
}
static void write_grant(const char *dir, int approved, const char *url) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddBoolToObject(root, "approved", approved); cJSON_AddStringToObject(root, "endpoint", url);
    char *json = cJSON_PrintUnformatted(root); char path[PATH_CAP]; assert(!path_join(path, dir, "capture-consent.json"));
    assert(!capture_atomic(path, json, strlen(json))); free(json); cJSON_Delete(root);
}
int main(int argc, char **argv) {
#ifdef SF_CAPTURE_TEST
    if (argc == 4 && !strcmp(argv[1], "--post")) {
        size_t n = 0; char *json = capture_read(argv[3], &n); if (!json) return 1;
        long status = 0; int rc = capture_post(argv[2], json, &status); free(json);
        printf("HTTP %ld\n", status); return rc ? 1 : 0;
    }
#endif
    assert(argc == 2); char dir[PATH_CAP], source[PATH_CAP];
    assert(strlen(argv[1]) < PATH_CAP); strcpy(dir, argv[1]); assert(!mkdir_tree(dir));
    assert(!path_join(source, dir, "probe-Π.lua")); assert(!capture_atomic(source, sample, strlen(sample)));
    char hash[65]; char *payload = capture_payload(sample, strlen(sample), source, hash); assert(payload);
    char identity[80]; snprintf(identity, sizeof(identity), "%s.json", hash);
    assert(capture_valid(payload, identity));
    char *bad_encoded = capture_decode("Zh==", &(size_t){0}); assert(!bad_encoded);
    cJSON *root = cJSON_Parse(payload), *record = cJSON_GetObjectItemCaseSensitive(root, "capture");
    assert(cJSON_IsArray(cJSON_GetObjectItemCaseSensitive(record, "snapshots")));
    assert(cJSON_IsArray(cJSON_GetObjectItemCaseSensitive(record, "pulls")));
    cJSON *api = cJSON_GetObjectItemCaseSensitive(record, "clientApi");
    assert(cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(api, "issecretvalue")));
    assert(cJSON_IsFalse(cJSON_GetObjectItemCaseSensitive(api, "IsSecretValue")));
    cJSON *future = cJSON_GetObjectItemCaseSensitive(record, "unknownFutureField");
    assert(!strcmp(capture_text(future, "text"), "line\né\"\\"));
    cJSON_ReplaceItemInObjectCaseSensitive(record, "people", cJSON_CreateObject());
    char *damaged = cJSON_PrintUnformatted(root); assert(!capture_valid(damaged, identity)); free(damaged);
    cJSON_Delete(root); free(payload);
    const char *bad[] = {
        "SimforeverProbeDB={version='x',value=os.execute('touch bad')}",
        "SimforeverProbeDB={version='x',a=1,a=2}",
        "SimforeverProbeDB={version='x',a=1e}",
        "SimforeverProbeDB={version='x',a=1e999}",
        "SimforeverProbeDB={version='x',a='\\999'}",
        "SimforeverProbeDB={version='x'", "SimforeverProbeDB={};evil=1",
    };
    for (size_t i = 0; i < sizeof(bad)/sizeof(*bad); ++i) assert(!capture_payload(bad[i], strlen(bad[i]), source, hash));
    char depth[512] = "SimforeverProbeDB="; for (int i = 0; i < 70; ++i) strcat(depth, "{");
    for (int i = 0; i < 70; ++i) strcat(depth, "}");
    assert(!capture_lua(depth, strlen(depth)));
    const char *sparse = "SimforeverProbeDB={version='x', t={[1]=1,[3]=3}, z={[0]='zero'}, list={true,false,nil}}";
    root = capture_lua(sparse, strlen(sparse)); assert(root);
    assert(cJSON_IsObject(cJSON_GetObjectItemCaseSensitive(root, "t")));
    assert(cJSON_IsObject(cJSON_GetObjectItemCaseSensitive(root, "z")));
    assert(cJSON_IsArray(cJSON_GetObjectItemCaseSensitive(root, "list"))); cJSON_Delete(root);

    char *options[] = {"test", "-config", dir, NULL}; CaptureCollector collector;
    assert(capture_init(&collector, 3, options));
    write_config(dir, source, 0, SF_ORIGIN);
    capture_tick(&collector, fake_post); assert(!attempts && !collector.active);
    write_config(dir, source, 1, SF_ORIGIN);
    write_grant(dir, 0, SF_ORIGIN "/v1/games/forever/probe");
    collector.poll = 0; capture_tick(&collector, fake_post); assert(!attempts && collector.active);
    CaptureFiles files; assert(!capture_files(collector.queue, &files)); assert(files.count == 1);
    uint64_t used = files.size; capture_files_free(&files);
    assert(!capture_enqueue(&collector, source, &used));
    assert(!capture_files(collector.queue, &files) && files.count == 1); capture_files_free(&files);
    collector.grant = 1; collector.poll = 0; fail_upload = 1;
    capture_tick(&collector, fake_post); assert(attempts == 1);
    assert(!capture_files(collector.queue, &files) && files.count == 1); capture_files_free(&files);
    capture_close(&collector);

    assert(capture_init(&collector, 3, options)); fail_upload = 0;
    capture_tick(&collector, fake_post);
    if (attempts != 2) {
        fprintf(stderr, "restart check: attempts=%d active=%d\n", attempts, collector.active);
#ifdef _WIN32
        fprintf(stderr, "Windows error=%lu\n", GetLastError());
#endif
    }
    assert(attempts == 2);
    collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == 2);
    capture_close(&collector); assert(capture_init(&collector, 3, options));
    capture_tick(&collector, fake_post); assert(attempts == 2);
    write_config(dir, source, 0, SF_ORIGIN); collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == 2);
    write_config(dir, source, 1, SF_ORIGIN "/other");
    write_grant(dir, 0, SF_ORIGIN "/other/v1/games/forever/probe");
    collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == 2);
    collector.grant = 1; collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == 3);

    /* Parsing errors retain the exact source locally and consume the archive
     * quota, but are never sent as a partial/guessed recording. */
    assert(!capture_atomic(source, bad[0], strlen(bad[0])));
    collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == 3);
    assert(!capture_files(collector.queue, &files) && files.count == 1 && files.size > used); capture_files_free(&files);
    const char *changed = "SimforeverProbeDB={version='0.33.0',unknownFutureField={}}";
    assert(!capture_atomic(source, changed, strlen(changed))); used = CAPTURE_QUOTA;
    assert(capture_enqueue(&collector, source, &used) == -1);
    assert(!capture_files(collector.queue, &files) && files.count == 1); capture_files_free(&files);
    capture_close(&collector);
    /* A failing oldest file must not prevent a newer capture from being
     * attempted, and both immutable files must survive a process restart. */
    char retry_dir[PATH_CAP]; assert(!path_join(retry_dir, dir, "retry")); assert(!mkdir_tree(retry_dir));
    char *retry_options[] = {"test", "-config", retry_dir, NULL};
    assert(!capture_atomic(source, sample, strlen(sample)));
    write_config(retry_dir, source, 1, SF_ORIGIN);
    write_grant(retry_dir, 1, SF_ORIGIN "/v1/games/forever/probe");
    assert(capture_init(&collector, 3, retry_options));
    fail_upload = 1; int before = attempts;
    capture_tick(&collector, fake_post); assert(attempts == before + 1);
    char first_hash[65]; strcpy(first_hash, posted_hash);
    assert(!capture_atomic(source, changed, strlen(changed)));
    collector.poll = 0; capture_tick(&collector, fake_post);
    assert(attempts == before + 2 && strcmp(first_hash, posted_hash));
    assert(!capture_files(collector.queue, &files) && files.count == 2); capture_files_free(&files);
    capture_close(&collector); assert(capture_init(&collector, 3, retry_options));
    fail_upload = 0; capture_tick(&collector, fake_post);
    collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == before + 4);
    collector.poll = 0; capture_tick(&collector, fake_post); assert(attempts == before + 4);
    capture_close(&collector);
    char *dry[] = {"test", "-config", dir, "-dry-run", NULL}; assert(!capture_init(&collector, 4, dry));
    char *values[] = {"test", "-name", "-dry-run", "-config", dir, NULL}; assert(capture_init(&collector, 5, values));
    puts("capture checks passed: full fields, strict Lua, queue, consent, independent receipts, restart, quota and diagnostics");
    return 0;
}
