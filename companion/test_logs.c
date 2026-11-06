#define SF_LOGS_TEST
#define main launcher_main
#include "launcher.c"
#undef main
#include <assert.h>
#ifndef _WIN32
static void make_dir(const char *path) { char *copy = malloc(strlen(path) + 1); assert(copy); strcpy(copy, path); assert(!mkdir_tree(copy)); free(copy); }
int main(int argc, char **argv) {
    assert(argc == 2); make_dir(argv[1]);
    /* The player's -logs=false keeps logs local; other flags do not. */
    char *off[] = {"x", "-logs=false"}, *off2[] = {"x", "--logs", "0"}, *on[] = {"x", "-name", "logs=false"};
    assert(logs_flag_off(2, off) && logs_flag_off(3, off2) && !logs_flag_off(3, on));
    char *diagnostic[] = {"x", "-once"};
    LogsSender s; assert(!logs_init(&s, 2, diagnostic) && !s.active);
    /* The game folder: a flavour inside a root, or a path already inside one. */
    char *root = malloc(PATH_CAP), *game = malloc(PATH_CAP), *out = malloc(PATH_CAP);
    assert(root && game && out && !addon_join(root, argv[1], "World of Warcraft"));
    assert(!addon_join(game, root, "_forever_")); make_dir(game);
    assert(!logs_game(root, out) && !strcmp(out, game));
    char *inside = malloc(PATH_CAP); assert(inside && !addon_join(inside, game, "Interface/AddOns"));
    assert(!logs_game(inside, out) && !strcmp(out, game)); free(inside);
    assert(logs_game(argv[1], out) == -1 && logs_game(NULL, out) == -1);
    /* The Simforever service takes appends; a custom server its own route. */
    char url[4096], display[128]; int custom = -1;
    cJSON *cfg = cJSON_CreateObject();
    assert(!logs_endpoint(cfg, url, &custom, display) && !custom && strstr(url, "/contributor-api/logs/anonymous"));
    cJSON_AddStringToObject(cfg, "apiBase", "https://sims.example/"); cJSON_AddStringToObject(cfg, "displayName", "Tester");
    assert(!logs_endpoint(cfg, url, &custom, display) && custom && !strcmp(url, "https://sims.example/v1/games/forever/logs") && !strcmp(display, "Tester"));
    cJSON_ReplaceItemInObject(cfg, "apiBase", cJSON_CreateString("http://sims.example"));
    assert(logs_endpoint(cfg, url, &custom, display) == -1); cJSON_Delete(cfg);
    /* Receipts and progress carry the Windows host's names. */
    char digest[65]; memset(digest, 'b', 64); digest[64] = 0;
    char who[65], name[200], *receipt = malloc(PATH_CAP), *expected = malloc(PATH_CAP), *progress = malloc(PATH_CAP);
    assert(!capture_hash_bytes("anonymous", 9, who)); snprintf(name, sizeof(name), "contribution-%s-%s.json", who, digest);
    assert(!logs_receipt(argv[1], "anonymous", digest, receipt) && !addon_join(expected, argv[1], name) && !strcmp(receipt, expected));
    assert(!logs_progress_path(argv[1], "anonymous", "WoWCombatLog-1.txt", progress));
    uint64_t had = 0; char sha[65]; assert(logs_progress(progress, &had, sha) == -1);
    logs_progress_save(progress, 123456789012ULL, digest);
    assert(!logs_progress(progress, &had, sha) && had == 123456789012ULL && !strcmp(sha, digest));
    /* The hash is kept while the file stands still and redone when it grows. */
    char *log = malloc(PATH_CAP); assert(log && !addon_join(log, game, "Logs")); make_dir(log);
    assert(!addon_join(log, game, "Logs/WoWCombatLog-110626_120000.txt") && !capture_atomic(log, "abc\n", 4));
    memset(&s, 0, sizeof(s)); struct stat st; assert(!stat(log, &st));
    const char *h1 = logs_hash(&s, log, &st); assert(h1 && !strcmp(h1, "edeaaff3f1774ad2888673770c6d64097e391bc362d7d6fb34982ddf0efd18cb"));
    assert(logs_hash(&s, log, &st) == h1);
    FILE *f = fopen(log, "ab"); assert(f && fputs("def\n", f) >= 0); fclose(f); assert(!stat(log, &st));
    const char *h2 = logs_hash(&s, log, &st); assert(h2 && !strcmp(h2, "924d391c158a46409fdff363063d718ea0bc00b14556f129984942af91233bbe"));
    logs_free(&s); assert(!s.files);
    /* End to end against a local service, where one is named. */
    const char *service = getenv("SF_LOGS_TEST_ORIGIN");
    if (service) {
        char *config = malloc(PATH_CAP); assert(config && !addon_join(config, argv[1], "config"));
        make_dir(config); cfg = cJSON_CreateObject(); cJSON_AddBoolToObject(cfg, "consentGranted", 1); cJSON_AddStringToObject(cfg, "wowPath", root);
        char *json = cJSON_PrintUnformatted(cfg); char *path = malloc(PATH_CAP); assert(json && path && !addon_join(path, config, "config.json") && !capture_atomic(path, json, strlen(json)));
        free(json); cJSON_Delete(cfg);
        const char *header = "11/4/2026 12:00:00.000  COMBAT_LOG_VERSION,22,ADVANCED_LOG_ENABLED,1,BUILD_VERSION,1.60.1,PROJECT_ID,18\n";
        assert(!capture_atomic(log, header, strlen(header)));
        f = fopen(log, "ab"); assert(f && fputs("{\"count\": 200}\n", f) >= 0); fclose(f);
        struct timespec old[2] = {{time(NULL) - 3600, 0}, {time(NULL) - 3600, 0}}; assert(!utimensat(AT_FDCWD, log, old, 0));
        char *args[] = {"x", "-config", config};
        LogsSender live; assert(logs_init(&live, 3, args));
        logs_tick(&live); assert(!stat(log, &st));
        const char *whole = logs_hash(&live, log, &st); assert(whole && !logs_receipt(config, "anonymous", whole, receipt) && addon_kind(receipt) == 1);
        /* The log grows; only the new line goes, and the service answers for the whole. */
        f = fopen(log, "ab"); assert(f && fputs("{\"count\": 300, \"shift\": 1000}\n", f) >= 0); fclose(f);
        assert(!utimensat(AT_FDCWD, log, old, 0)); live.next_poll = 0; live.next_upload = 0;
        logs_tick(&live); assert(!stat(log, &st));
        const char *grown = logs_hash(&live, log, &st); assert(grown && !logs_receipt(config, "anonymous", grown, receipt) && addon_kind(receipt) == 1);
        assert(!logs_progress_path(config, "anonymous", "WoWCombatLog-110626_120000.txt", progress) && !logs_progress(progress, &had, sha));
        assert(had == (uint64_t)st.st_size && !strcmp(sha, grown));
        logs_free(&live); free(config); free(path);
        puts("POSIX combat log service checks passed.");
    }
    free(root); free(game); free(out); free(receipt); free(expected); free(progress); free(log);
    puts("POSIX combat log checks passed: player opt-out, game folder, endpoints, shared receipt names, progress and hash cache");
    return 0;
}
#else
int main(void) { return 0; }
#endif
