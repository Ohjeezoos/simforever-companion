#define SF_DESKTOP_TEST
#define main sf_launcher_main
#define wmain sf_launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
#include <assert.h>
int main(int argc, char **argv) {
    assert(argc == 2 && !mkdir_tree(argv[1]));
    FILE *spool = contributor_spool(argv[1]); assert(spool && fwrite("private-log", 1, 11, spool) == 11);
    rewind(spool); char spool_bytes[12] = {0}; assert(fread(spool_bytes, 1, 11, spool) == 11 && !strcmp(spool_bytes, "private-log")); fclose(spool);
    char token[65], copied[65] = {0}; memset(token, 'a', 64); token[64] = 0;
    assert(!contributor_load(argv[1], copied));
    assert(contributor_endpoint(argv[1]));
    cJSON *cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "apiBase", "https://private.example"); assert(!desktop_json(argv[1], "config.json", cfg)); cJSON_Delete(cfg);
    assert(!contributor_endpoint(argv[1]));
    cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "apiBase", SF_ORIGIN "/"); assert(!desktop_json(argv[1], "config.json", cfg)); cJSON_Delete(cfg);
    assert(contributor_endpoint(argv[1]));
    assert(contributor_save(argv[1], "invalid") == -1);
    assert(!contributor_save(argv[1], token));
    assert(contributor_load(argv[1], copied) && !strcmp(token, copied));
    char *path = malloc(PATH_CAP); size_t n = 0; assert(path && !addon_join(path, argv[1], "contributor.dat"));
    char *encrypted = capture_read(path, &n); assert(encrypted && n > 64);
    for (size_t i = 0; i + 64 <= n; ++i) assert(memcmp(encrypted + i, token, 64));
    encrypted[n/2] ^= 0x55; assert(!capture_atomic(path, encrypted, n));
    assert(!contributor_load(argv[1], copied));
    free(encrypted);
    assert(!contributor_save(argv[1], token));
    char *receipt = malloc(PATH_CAP); assert(receipt && !contributor_receipt(argv[1], token, token, receipt));
    assert(!strstr(receipt, token) || strstr(receipt, token) == receipt + strlen(receipt) - 69);
    char *anonymous = malloc(PATH_CAP), digest[65]; memset(digest, 'b', 64); digest[64] = 0;
    assert(anonymous && !contributor_receipt(argv[1], NULL, digest, anonymous) && strcmp(anonymous, receipt));
    assert(!contributor_legacy_sent(argv[1], digest));
    cfg = cJSON_CreateObject(); cJSON *entries = cJSON_AddObjectToObject(cfg, "entries"), *entry = cJSON_AddObjectToObject(entries, "log:C:\\Logs\\WoWCombatLog.txt");
    cJSON_AddStringToObject(entry, "hash", digest); cJSON_AddObjectToObject(entries, "probe:x"); assert(!desktop_json(argv[1], "state.json", cfg)); cJSON_Delete(cfg);
    assert(contributor_legacy_sent(argv[1], digest)); digest[0] = 'c'; assert(!contributor_legacy_sent(argv[1], digest)); free(anonymous);
    long status = 0; assert(!contributor_request("device/status", "invalid", NULL, 0, &status));
    assert(!status);
    wchar_t *w = wide_path(path); assert(w && DeleteFileW(w)); free(w); free(path); free(receipt);
    /* Quiet is the companion's own observation of the true size, not the
     * directory entry an open log leaves stale. */
    char *log = malloc(PATH_CAP); assert(log && !addon_join(log, argv[1], "WoWCombatLog-test.txt"));
    assert(!capture_atomic(log, "12345", 5)); uint64_t real = 0;
    assert(!contributor_true_size(log, &real) && real == 5);
    ContributionLoop loop = {0};
    ULONGLONG first = contributor_unchanged_since(&loop, log, real, 0);
    assert(GetTickCount64() - first < CONTRIBUTOR_QUIET_MS);
    assert(contributor_unchanged_since(&loop, log, real, 1) == first);
    Sleep(20); assert(contributor_unchanged_since(&loop, log, real + 1, 1) > first);
    char other[16] = "closed.txt";
    assert(GetTickCount64() - contributor_unchanged_since(&loop, other, 3, 1) >= CONTRIBUTOR_QUIET_MS
           || GetTickCount64() < CONTRIBUTOR_QUIET_MS);
    contributor_loop_free(&loop); assert(!loop.growth);
    /* What the service acknowledged of a log, per account and per log. */
    char *progress = malloc(PATH_CAP), *anon = malloc(PATH_CAP), *later = malloc(PATH_CAP), sha[65]; uint64_t had = 0;
    assert(progress && anon && later && !contributor_progress_path(argv[1], token, "WoWCombatLog-1.txt", progress));
    assert(!contributor_progress_path(argv[1], NULL, "WoWCombatLog-1.txt", anon) && strcmp(progress, anon));
    assert(!contributor_progress_path(argv[1], token, "WoWCombatLog-2.txt", later) && strcmp(progress, later));
    assert(contributor_progress(progress, &had, sha) == -1);
    contributor_progress_save(progress, 123456789012ULL, digest);
    assert(!contributor_progress(progress, &had, sha) && had == 123456789012ULL && !strcmp(sha, digest));
    const char *bad = "{\"size\":5,\"sha256\":\"XYZ\"}";
    assert(!capture_atomic(progress, bad, strlen(bad)) && contributor_progress(progress, &had, sha) == -1);
    assert(!contributor_request_base("logs", NULL, NULL, 0, "not-a-digest", &status) && !status);
    w = wide_path(progress); assert(w && DeleteFileW(w)); free(w); free(progress); free(anon); free(later);
    w = wide_path(log); assert(w && DeleteFileW(w)); free(w); free(log);
    puts("Contributor credential checks passed."); return 0;
}
