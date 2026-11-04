/* Install the bundled probe independently of upload consent. */
#ifndef SF_ADDON_H
#define SF_ADDON_H
typedef struct { const char *name; const unsigned char *data; size_t size; const char *hash; } AddonFile;
#include "probe_payload.h"

typedef struct { char config[PATH_CAP], explicit_wow[PATH_CAP]; int active, checked; } AddonManager;

static int addon_join(char *out, const char *base, const char *name) {
    size_t a = strlen(base), b = strlen(name);
    if (a >= PATH_CAP || b >= PATH_CAP || a + b + 2 > PATH_CAP) return -1;
    char *copy = malloc(a + b + 2); if (!copy) return -1;
    memcpy(copy, base, a); copy[a] = '/'; memcpy(copy + a + 1, name, b + 1);
    memcpy(out, copy, a + b + 2); free(copy); return 0;
}

static int addon_kind(const char *path) {
#ifdef _WIN32
    wchar_t *w = wide_path(path); if (!w) return -1;
    DWORD attrs = GetFileAttributesW(w), error = GetLastError(); free(w);
    if (attrs == INVALID_FILE_ATTRIBUTES)
        return error == ERROR_FILE_NOT_FOUND || error == ERROR_PATH_NOT_FOUND ? 0 : -1;
    if (attrs & FILE_ATTRIBUTE_REPARSE_POINT) return 3;
    return attrs & FILE_ATTRIBUTE_DIRECTORY ? 2 : 1;
#else
    struct stat st;
    if (lstat(path, &st)) return errno == ENOENT ? 0 : -1;
    return S_ISLNK(st.st_mode) ? 3 : S_ISDIR(st.st_mode) ? 2 : S_ISREG(st.st_mode) ? 1 : -1;
#endif
}
/* Blizzard records the actual product independently of the folder name. */
static int addon_is_forever(const char *game) {
    if (addon_kind(game) != 2) return 0;
    char *path = malloc(PATH_CAP); if (!path) return 0;
    size_t n = 0;
    char *text = !addon_join(path, game, ".flavor.info") && addon_kind(path) == 1
        ? capture_read_limit(path, &n, 4096) : NULL;
    int valid = 0;
    if (text && !memchr(text, 0, n)) {
        char *header = text;
        if (n >= 3 && !memcmp(header, "\xef\xbb\xbf", 3)) header += 3;
        char *end = strchr(header, '\n');
        if (end) {
            *end++ = 0;
            size_t len = strlen(header); if (len && header[len - 1] == '\r') header[len - 1] = 0;
            char *tail = end; while (*tail && *tail != '\r' && *tail != '\n') ++tail;
            char saved = *tail; *tail = 0;
            valid = !strcmp(header, "Product Flavor!STRING:0") && !strcmp(end, "wowf");
            *tail = saved;
            while (*tail == '\r' || *tail == '\n') ++tail;
            if (*tail) valid = 0;
        }
    }
    free(text);
    if (valid) {
        valid = !addon_join(path, game, "Wow.exe") && addon_kind(path) == 1;
#ifdef __APPLE__
        if (!valid && !addon_join(path, game, "World of Warcraft.app/Contents/MacOS/World of Warcraft"))
            valid = addon_kind(path) == 1;
#endif
    }
    free(path); return valid;
}
static int addon_directory(const char *path) {
    int kind = addon_kind(path);
    if (!kind && !mkdir_one(path)) kind = addon_kind(path);
    return kind == 2 ? 0 : -1;
}
static int addon_move(const char *from, const char *to) {
    if (addon_kind(to) != 0) return -1;
#ifdef _WIN32
    wchar_t *a = wide_path(from), *b = wide_path(to);
    int ok = a && b && MoveFileExW(a, b, MOVEFILE_WRITE_THROUGH);
    free(a); free(b); return ok ? 0 : -1;
#else
    return rename(from, to);
#endif
}
static void addon_remove_stage(const char *stage) {
    for (size_t i = 0; i < sizeof(sf_probe_files) / sizeof(*sf_probe_files); ++i) {
        char path[PATH_CAP];
        if (!addon_join(path, stage, sf_probe_files[i].name) && addon_kind(path) == 1) remove_file(path);
    }
#ifdef _WIN32
    wchar_t *w = wide_path(stage); if (w) RemoveDirectoryW(w); free(w);
#else
    (void)rmdir(stage);
#endif
}
static int addon_matches(const char *dir) {
    for (size_t i = 0; i < sizeof(sf_probe_files) / sizeof(*sf_probe_files); ++i) {
        const AddonFile *file = &sf_probe_files[i]; char path[PATH_CAP];
        Release r = {0}; r.size = file->size; strcpy(r.hash, file->hash);
        if (addon_join(path, dir, file->name) || addon_kind(path) != 1 || verify(path, &r)) return 0;
    }
    return 1;
}
static int addon_toc_version(const char *dir, char version[32]) {
    char path[PATH_CAP]; size_t n = 0;
    if (addon_join(path, dir, "SimforeverProbe.toc") || addon_kind(path) != 1) return -1;
    char *text = capture_read_limit(path, &n, 65536); if (!text) return -1;
    int found = -1;
    for (char *line = text; line && *line;) {
        char *next = strchr(line, '\n'); if (next) *next++ = 0;
        size_t len = strlen(line); if (len && line[len - 1] == '\r') line[--len] = 0;
        if (!strncmp(line, "## Version: ", 12) && len - 12 < 32) {
            unsigned parts[3];
            if (!version_parts(line + 12, parts)) { strcpy(version, line + 12); found = 0; }
            break;
        }
        line = next;
    }
    free(text); return found;
}
static int addon_install(const char *game) {
    if (!addon_is_forever(game)) return -1;
    char (*paths)[PATH_CAP] = calloc(8, sizeof(*paths)); if (!paths) return -1;
    char *normalized = paths[0], *interface_dir = paths[1], *addons = paths[2], *installed = paths[3];
    char *stage = paths[4], *backup = paths[5], *lock_path = paths[6], *file_path = paths[7];
    if (strlen(game) >= PATH_CAP) { free(paths); return -1; }
    strcpy(normalized, game);
    for (char *p = normalized; *p; ++p) if (*p == '\\') *p = '/';
    size_t n = strlen(normalized); while (n > 1 && normalized[n - 1] == '/') normalized[--n] = 0;
    if (addon_join(interface_dir, normalized, "Interface") || addon_directory(interface_dir) ||
        addon_join(addons, interface_dir, "AddOns") || addon_directory(addons) ||
        addon_join(installed, addons, "SimforeverProbe") || addon_join(lock_path, addons, ".SimforeverProbe.install.lock")) { free(paths); return -1; }
    if (addon_kind(lock_path) != 0 && addon_kind(lock_path) != 1) { free(paths); return -1; }
#ifdef _WIN32
    wchar_t *w = wide_path(lock_path);
    HANDLE lock = w ? CreateFileW(w, GENERIC_READ | GENERIC_WRITE, 0, NULL, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL) : INVALID_HANDLE_VALUE;
    free(w); if (lock == INVALID_HANDLE_VALUE) { free(paths); return -1; }
#else
    int lock = open(lock_path, O_RDWR | O_CREAT | O_NOFOLLOW, 0600);
    if (lock < 0) { free(paths); return -1; }
    if (flock(lock, LOCK_EX | LOCK_NB)) { close(lock); free(paths); return -1; }
#endif
    int rc = -1, exists = addon_kind(installed); char current[32] = "unknown";
    stage[0] = 0;
    if (exists != 0 && exists != 2) goto done;
    if (exists == 2 && !addon_toc_version(installed, current) && compare_version(current, sf_probe_version) > 0) { rc = 0; goto done; }
    if (exists == 2 && addon_matches(installed)) { rc = 0; goto done; }
    char suffix[128]; snprintf(suffix, sizeof(suffix), ".SimforeverProbe.install-%lu", process_id());
    if (addon_join(stage, addons, suffix) || addon_kind(stage) != 0 || mkdir_one(stage)) { stage[0] = 0; goto done; }
    for (size_t i = 0; i < sizeof(sf_probe_files) / sizeof(*sf_probe_files); ++i) {
        const AddonFile *file = &sf_probe_files[i];
        if (addon_join(file_path, stage, file->name)) goto done;
        FILE *f = new_file(file_path); if (!f) goto done;
        int failed = fwrite(file->data, 1, file->size, f) != file->size;
        if (fclose(f)) failed = 1;
        if (failed) goto done;
    }
    if (!addon_matches(stage)) goto done;
    if (exists) {
        snprintf(suffix, sizeof(suffix), ".SimforeverProbe.backup-%s-%lu-%lld", current, process_id(), (long long)time(NULL));
        if (addon_join(backup, addons, suffix) || addon_move(installed, backup)) goto done;
    }
    if (addon_move(stage, installed)) {
        if (exists && addon_move(backup, installed)) fprintf(stderr, "Addon installation failed; previous files remain at %s\n", backup);
        goto done;
    }
    stage[0] = 0; rc = 1;
    fprintf(stderr, "Simforever Probe %s installed in %s. Log in or reload the interface to load it.\n", sf_probe_version, normalized);
done:
    if (stage[0]) addon_remove_stage(stage);
#ifdef _WIN32
    CloseHandle(lock);
#else
    close(lock);
#endif
    free(paths); return rc;
}
static int addon_try_root(const char *root) {
    if (!root || !*root || strlen(root) >= PATH_CAP) return 0;
    char (*paths)[PATH_CAP] = calloc(2, sizeof(*paths)); if (!paths) return -1;
    char *path = paths[0], *child = paths[1]; strcpy(path, root);
    for (char *p = path; *p; ++p) if (*p == '\\') *p = '/';
    size_t n = strlen(path); while (n > 1 && path[n - 1] == '/') path[--n] = 0;
    if (addon_is_forever(path)) {
        int rc = addon_install(path); free(paths); return rc;
    }
    const char *inside[] = {"/_forever_/", "/_classic_beta_/"};
    for (size_t i = 0; i < sizeof(inside) / sizeof(*inside); ++i) {
        char *part = strstr(path, inside[i]);
        if (part) { part[strlen(inside[i]) - 1] = 0; int rc = addon_install(path); free(paths); return rc; }
    }
    int result = 0;
    const char *flavors[] = {"_forever_", "_classic_beta_"};
    for (size_t i = 0; i < sizeof(flavors) / sizeof(*flavors); ++i) {
        if (addon_join(child, path, flavors[i])) continue;
        if (addon_kind(child) == 2) { int rc = addon_install(child); if (rc > result) result = rc; if (rc < 0 && !result) result = rc; }
    }
    free(paths); return result;
}
static int addon_init(AddonManager *a, int argc, char **argv) {
    memset(a, 0, sizeof(*a)); if (capture_default_config(a->config)) return 0;
    for (int i = 1; i < argc; ++i) {
        const char *p = argv[i]; if (*p != '-') break;
        ++p; if (*p == '-') ++p; if (!*p) break;
        size_t len = strcspn(p, "=");
        const char *diagnostics[] = {"show", "once", "dry-run", "print-payload", "version", "h", "help"};
        for (size_t j = 0; j < sizeof(diagnostics) / sizeof(*diagnostics); ++j)
            if (strlen(diagnostics[j]) == len && !strncmp(p, diagnostics[j], len)) return 0;
        const char *value = p[len] == '=' ? p + len + 1 : NULL;
        const char *values[] = {"config", "wow", "api", "game", "name", "interval"};
        for (size_t j = 0; j < sizeof(values) / sizeof(*values); ++j) {
            if (strlen(values[j]) != len || strncmp(p, values[j], len)) continue;
            if (!value && i + 1 < argc) value = argv[++i];
            if (!value) return 0;
            char *out = !strcmp(values[j], "config") ? a->config : !strcmp(values[j], "wow") ? a->explicit_wow : NULL;
            if (out) { if (strlen(value) >= PATH_CAP) return 0; strcpy(out, value); }
            break;
        }
    }
    a->active = 1; return 1;
}
static int addon_release_args(int argc, char **argv, char ***out, char **game) {
    *out = argv; *game = NULL;
    AddonManager *a = calloc(1, sizeof(*a)); if (!a) return argc;
    if (!addon_init(a, argc, argv) || a->explicit_wow[0]) { free(a); return argc; }
    for (int i = 1; i < argc; ++i) if (!strcmp(argv[i], "--")) { free(a); return argc; }
    cJSON *cfg = capture_config(a->config); free(a);
    const char *old = capture_text(cfg, "wowPath");
    char *path = old && strlen(old) < PATH_CAP ? malloc(PATH_CAP) : NULL;
    if (!path) { cJSON_Delete(cfg); return argc; }
    strcpy(path, old); cJSON_Delete(cfg);
    for (char *p = path; *p; ++p) if (*p == '\\') *p = '/';
    size_t n = strlen(path); while (n && path[n - 1] == '/') path[--n] = 0;
    char *last = strrchr(path, '/');
    if (!last || strcmp(last, "/_classic_beta_")) { free(path); return argc; }
    *last = 0;
    char *release = malloc(PATH_CAP);
    if (!release || addon_join(release, path, "_forever_") || !addon_is_forever(release)) { free(path); free(release); return argc; }
    free(path);
    char **args = calloc((size_t)argc + 3, sizeof(*args));
    if (!args) { free(release); return argc; }
    for (int i = 0; i < argc; ++i) args[i] = argv[i];
    args[argc] = "-wow"; args[argc + 1] = release;
    *out = args; *game = release; return argc + 2;
}
static void addon_start(AddonManager *a) {
    if (!a->active || a->checked) return;
    a->checked = 1;
    char *settings = malloc(PATH_CAP); size_t settings_size = 0;
    char *text = settings && !addon_join(settings, a->config, "addon-settings.json") ? capture_read(settings, &settings_size) : NULL;
    free(settings);
    cJSON *prefs = text ? cJSON_ParseWithLengthOpts(text, settings_size + 1, NULL, 1) : NULL;
    int disabled = cJSON_IsFalse(cJSON_GetObjectItemCaseSensitive(prefs, "enabled"));
    free(text); cJSON_Delete(prefs);
    if (disabled) return;
    cJSON *cfg = capture_config(a->config);
    const char *wow = a->explicit_wow[0] ? a->explicit_wow : capture_text(cfg, "wowPath");
    if (wow && *wow) { (void)addon_try_root(wow); cJSON_Delete(cfg); return; }
    cJSON_Delete(cfg);
#ifdef _WIN32
    wchar_t *w = calloc(PATH_CAP, sizeof(*w));
    char (*paths)[PATH_CAP] = calloc(2, sizeof(*paths));
    if (!w || !paths) { free(w); free(paths); return; }
    char *utf = paths[0], *path = paths[1];
    const wchar_t *names[] = {L"ProgramFiles(x86)", L"ProgramFiles", L"ProgramW6432"};
    for (size_t i = 0; i < sizeof(names) / sizeof(*names); ++i) {
        DWORD n = GetEnvironmentVariableW(names[i], w, PATH_CAP);
        if (n && n < PATH_CAP && WideCharToMultiByte(CP_UTF8, 0, w, -1, utf, PATH_CAP, NULL, NULL) &&
            !addon_join(path, utf, "World of Warcraft")) (void)addon_try_root(path);
    }
    const wchar_t *keys[] = {
        L"SOFTWARE\\WOW6432Node\\Blizzard Entertainment\\World of Warcraft",
        L"SOFTWARE\\Blizzard Entertainment\\World of Warcraft",
        L"SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\World of Warcraft"
    };
    for (size_t i = 0; i < sizeof(keys) / sizeof(*keys); ++i) {
        DWORD bytes = PATH_CAP * (DWORD)sizeof(*w);
        const wchar_t *field = i == 2 ? L"InstallLocation" : L"InstallPath";
        if (RegGetValueW(HKEY_LOCAL_MACHINE, keys[i], field, RRF_RT_REG_SZ, NULL, w, &bytes) != ERROR_SUCCESS ||
            !WideCharToMultiByte(CP_UTF8, 0, w, -1, utf, PATH_CAP, NULL, NULL)) continue;
        for (char *p = utf; *p; ++p) if (*p == '\\') *p = '/';
        size_t length = strlen(utf); while (length && utf[length - 1] == '/') utf[--length] = 0;
        char *last = strrchr(utf, '/');
        if (last && last[1] == '_' && length > 1 && utf[length - 1] == '_') *last = 0;
        (void)addon_try_root(utf);
    }
    char drives[104]; DWORD drive_count = GetLogicalDriveStringsA(sizeof(drives), drives);
    if (!drive_count || drive_count >= sizeof(drives)) drives[0] = 0;
    const char *dirs[] = {"/Program Files (x86)/World of Warcraft", "/Program Files/World of Warcraft", "/Games/World of Warcraft", "/World of Warcraft"};
    for (const char *drive = drives; *drive; drive += strlen(drive) + 1)
        for (size_t j = 0; j < sizeof(dirs) / sizeof(*dirs); ++j) {
            snprintf(path, PATH_CAP, "%.2s%s", drive, dirs[j]); (void)addon_try_root(path);
        }
    free(w); free(paths);
#else
    (void)addon_try_root("/Applications/World of Warcraft");
    const char *drives[] = {"/mnt/c", "/mnt/d", "/mnt/e", "/mnt/f"};
    const char *dirs[] = {"/Program Files (x86)/World of Warcraft", "/Program Files/World of Warcraft", "/Games/World of Warcraft", "/World of Warcraft"};
    for (size_t i = 0; i < sizeof(drives) / sizeof(*drives); ++i)
        for (size_t j = 0; j < sizeof(dirs) / sizeof(*dirs); ++j) { char path[PATH_CAP]; snprintf(path, sizeof(path), "%s%s", drives[i], dirs[j]); (void)addon_try_root(path); }
    const char *home = getenv("HOME");
    if (home) { char path[PATH_CAP]; if (!addon_join(path, home, ".wine/drive_c/Program Files (x86)/World of Warcraft")) (void)addon_try_root(path); }
#endif
}
#endif
