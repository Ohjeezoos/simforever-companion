#define main sf_launcher_main
#define wmain sf_launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
#include <assert.h>

static void addon_test_write(const char *path, const char *value) {
    assert(!capture_atomic(path, value, strlen(value)));
}
static void addon_test_game(const char *game, const char *product) {
    char *path = malloc(PATH_CAP); assert(path);
    assert(!path_join(path, game, ".flavor.info")); addon_test_write(path, product);
    assert(!path_join(path, game, "Wow.exe")); addon_test_write(path, "fixture executable"); free(path);
}
static void addon_test_text(const char *path, const char *expected) {
    size_t n = 0; char *text = capture_read(path, &n);
    assert(text && n == strlen(expected) && !memcmp(text, expected, n)); free(text);
}
static int addon_test_backup(const char *dir, const char *name) {
#ifdef _WIN32
    char pattern[PATH_CAP]; assert(!path_join(pattern, dir, ".SimforeverProbe.backup-*"));
    wchar_t *w = wide_path(pattern); WIN32_FIND_DATAW data;
    HANDLE find = FindFirstFileW(w, &data); free(w);
    if (find == INVALID_HANDLE_VALUE) return 0;
    int found = 0;
    do {
        char entry[PATH_CAP], path[PATH_CAP], file[PATH_CAP];
        if (WideCharToMultiByte(CP_UTF8, 0, data.cFileName, -1, entry, PATH_CAP, NULL, NULL) &&
            !path_join(path, dir, entry) && !path_join(file, path, name) && addon_kind(file) == 1) found = 1;
    } while (FindNextFileW(find, &data));
    FindClose(find); return found;
#else
    DIR *d = opendir(dir); assert(d); struct dirent *entry; int found = 0;
    while ((entry = readdir(d))) {
        char path[PATH_CAP], file[PATH_CAP];
        if (!strncmp(entry->d_name, ".SimforeverProbe.backup-", 24) &&
            !path_join(path, dir, entry->d_name) && !path_join(file, path, name) && addon_kind(file) == 1) found = 1;
    }
    closedir(d); return found;
#endif
}
int main(int argc, char **argv) {
    assert(argc == 2);
    char game[PATH_CAP], installed[PATH_CAP], addons[PATH_CAP], toc[PATH_CAP], core[PATH_CAP];
    char settings[PATH_CAP], log[PATH_CAP], other[PATH_CAP], custom[PATH_CAP];
    assert(!path_join(game, argv[1], "Game with spaces-Π/_forever_") && !mkdir_tree(game));
    assert(!path_join(settings, game, "WTF/Account/test/SavedVariables") && !mkdir_tree(settings));
    assert(!path_join(custom, settings, "SimforeverProbe.lua")); addon_test_write(custom, "existing recording");
    strcpy(settings, custom);
    assert(!path_join(log, game, "Logs") && !mkdir_tree(log));
    assert(!path_join(custom, log, "WoWCombatLog.txt")); addon_test_write(custom, "existing combat log"); strcpy(log, custom);
    assert(!path_join(other, game, "Interface/AddOns/OtherAddon") && !mkdir_tree(other));
    assert(!path_join(custom, other, "settings.lua")); addon_test_write(custom, "other addon"); strcpy(other, custom);
    addon_test_game(game, "Product Flavor!STRING:0\nwowf\n");
    assert(addon_install(game) == 1);
    assert(!path_join(addons, game, "Interface/AddOns") && !path_join(installed, addons, "SimforeverProbe"));
    assert(addon_matches(installed)); assert(addon_install(game) == 0);
    assert(!path_join(toc, installed, "SimforeverProbe.toc") && !path_join(core, installed, "Core.lua"));
    addon_test_write(toc, "## Version: 0.1.0\n");
    assert(!path_join(custom, installed, "local-note.txt")); addon_test_write(custom, "keep my note");
    addon_test_write(core, "local modification");
    assert(addon_install(game) == 1 && addon_matches(installed));
    assert(addon_test_backup(addons, "local-note.txt"));
    addon_test_text(settings, "existing recording"); addon_test_text(log, "existing combat log"); addon_test_text(other, "other addon");
    char collision[PATH_CAP], suffix[100];
    snprintf(suffix, sizeof(suffix), ".SimforeverProbe.install-%lu", process_id());
    assert(!path_join(collision, addons, suffix) && !mkdir_tree(collision));
    assert(!path_join(custom, collision, "keep.txt")); addon_test_write(custom, "not our staging directory");
    addon_test_write(core, "unchanged after failed repair");
    assert(addon_install(game) == -1); addon_test_text(core, "unchanged after failed repair"); addon_test_text(custom, "not our staging directory");
    addon_test_write(toc, "## Version: 999.0.0\n"); addon_test_write(core, "newer addon");
    assert(addon_install(game) == 0); addon_test_text(core, "newer addon");

    char retail[PATH_CAP]; assert(!path_join(retail, argv[1], "_retail_") && !mkdir_tree(retail));
    assert(addon_install(retail) == -1); assert(!path_join(custom, retail, "Interface") && addon_kind(custom) == 0);
    char cfgdir[PATH_CAP]; assert(!path_join(cfgdir, argv[1], "config") && !mkdir_tree(cfgdir));
    char *dry[] = {"app", "-config", cfgdir, "-dry-run", NULL}; AddonManager manager;
    assert(!addon_init(&manager, 4, dry));
    char *named[] = {"app", "-name", "-dry-run", "-config", cfgdir, "-wow", game, NULL};
    assert(addon_init(&manager, 7, named)); assert(!strcmp(manager.explicit_wow, game));
    char fresh[PATH_CAP]; assert(!path_join(fresh, argv[1], "fresh/_forever_") && !mkdir_tree(fresh));
    addon_test_game(fresh, "Product Flavor!STRING:0\nwowf\n");
    cJSON *cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "wowPath", fresh);
    char *json = cJSON_PrintUnformatted(cfg); cJSON_Delete(cfg);
    char cfgpath[PATH_CAP]; assert(!path_join(cfgpath, cfgdir, "config.json")); addon_test_write(cfgpath, json); free(json);
    char prefs[PATH_CAP]; assert(!path_join(prefs, cfgdir, "addon-settings.json")); addon_test_write(prefs, "{\"enabled\":false}");
    char *normal[] = {"app", "-config", cfgdir, NULL}; assert(addon_init(&manager, 3, normal)); addon_start(&manager);
    assert(!path_join(custom, fresh, "Interface") && addon_kind(custom) == 0);
    addon_test_text(prefs, "{\"enabled\":false}"); remove_file(prefs);
    cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "wowPath", fresh); cJSON_AddStringToObject(cfg, "unknownSetting", "keep");
    json = cJSON_PrintUnformatted(cfg); cJSON_Delete(cfg); addon_test_write(cfgpath, json);
    /* Changing preferences during the session does not trigger addon work. */
    addon_start(&manager);
    assert(!path_join(custom, fresh, "Interface") && addon_kind(custom) == 0);
    assert(addon_init(&manager, 3, normal)); addon_start(&manager);
    assert(!path_join(custom, fresh, "Interface/AddOns/SimforeverProbe") && addon_matches(custom));
    addon_test_text(cfgpath, json); free(json);
    char beta[PATH_CAP], release[PATH_CAP];
    assert(!path_join(beta, argv[1], "migration/_classic_beta_") && !mkdir_tree(beta));
    assert(!path_join(release, argv[1], "migration/_forever_") && !mkdir_tree(release));
    addon_test_game(release, "Product Flavor!STRING:0\nwowf\n");
    for (char *p = release; *p; ++p) if (*p == '\\') *p = '/';
    cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "wowPath", beta);
    cJSON_AddBoolToObject(cfg, "consentGranted", 0); cJSON_AddStringToObject(cfg, "apiBase", "https://custom.example");
    json = cJSON_PrintUnformatted(cfg); cJSON_Delete(cfg); addon_test_write(cfgpath, json);
    char **forwarded = NULL, *release_path = NULL;
    assert(addon_release_args(3, normal, &forwarded, &release_path) == 5);
    assert(!strcmp(forwarded[3], "-wow") && !strcmp(forwarded[4], release));
    addon_test_text(cfgpath, json); free(forwarded); free(release_path); free(json);
    assert(addon_release_args(7, named, &forwarded, &release_path) == 7 && !release_path);
    assert(addon_release_args(4, dry, &forwarded, &release_path) == 4 && !release_path);
#ifndef _WIN32
    char linked[PATH_CAP], external[PATH_CAP], linkpath[PATH_CAP];
    assert(!path_join(linked, argv[1], "linked/_forever_") && !mkdir_tree(linked));
    assert(!path_join(external, argv[1], "external") && !mkdir_tree(external));
    assert(!path_join(linkpath, linked, "Interface") && !symlink(external, linkpath));
    addon_test_game(linked, "Product Flavor!STRING:0\nwowf\n");
    assert(addon_install(linked) == -1);
    assert(!path_join(custom, external, "AddOns") && addon_kind(custom) == 0);
    assert(!path_join(linked, argv[1], "linked-target/_forever_/Interface/AddOns") && !mkdir_tree(linked));
    assert(!path_join(linkpath, linked, "SimforeverProbe") && !symlink(external, linkpath));
    assert(!path_join(linked, argv[1], "linked-target/_forever_")); addon_test_game(linked, "Product Flavor!STRING:0\nwowf\n"); assert(addon_install(linked) == -1);
#endif
    puts("addon checks passed: fresh install, repair, backup, no downgrade, passive install, opt-out, diagnostic isolation and preserved recordings/config");
    return 0;
}
