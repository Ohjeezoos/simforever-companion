/* Client discovery never follows directory links or reparse points. */
#ifndef SF_DISCOVERY_H
#define SF_DISCOVERY_H
typedef int (*DiscoveryCancel)(void *);
typedef void (*DiscoveryProgress)(void *, const char *);
typedef struct SearchNode { char *path; struct SearchNode *next; } SearchNode;
static int discovery_push(SearchNode **stack, const char *path) {
    SearchNode *n = calloc(1, sizeof(*n)); if (!n) return -1;
    n->path = malloc(strlen(path) + 1);
    if (!n->path) { free(n); return -1; }
    strcpy(n->path, path); n->next = *stack; *stack = n; return 0;
}
/* 1 found, 0 exhausted, -1 cancelled, -2 allocation/path limit failure.
 * Access-denied directories are skipped, not reported as searched. */
static int discovery_tree(const char *root, char *out, DiscoveryCancel cancel,
                          DiscoveryProgress progress, void *ctx) {
    SearchNode *stack = NULL; int result = 0;
    char (*paths)[PATH_CAP] = calloc(2, sizeof(*paths));
    if (!paths || strlen(root) >= PATH_CAP || discovery_push(&stack, root)) { free(paths); return -2; }
    while (stack) {
        if (cancel && cancel(ctx)) { result = -1; break; }
        SearchNode *node = stack; stack = node->next;
        strcpy(paths[0], node->path); free(node->path); free(node);
        if (addon_kind(paths[0]) != 2) continue;
        if (progress) progress(ctx, paths[0]);
        if (addon_is_forever(paths[0])) { strcpy(out, paths[0]); result = 1; break; }
#ifdef _WIN32
        if (addon_join(paths[1], paths[0], "*")) { result = -2; break; }
        wchar_t *pattern = wide_path(paths[1]); WIN32_FIND_DATAW data = {0};
        HANDLE find = pattern ? FindFirstFileExW(pattern, FindExInfoBasic, &data, FindExSearchLimitToDirectories, NULL, FIND_FIRST_EX_LARGE_FETCH) : INVALID_HANDLE_VALUE;
        free(pattern); if (find == INVALID_HANDLE_VALUE) continue;
        do {
            if (cancel && cancel(ctx)) { result = -1; break; }
            if (!(data.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) || (data.dwFileAttributes & FILE_ATTRIBUTE_REPARSE_POINT) ||
                !wcscmp(data.cFileName, L".") || !wcscmp(data.cFileName, L"..")) continue;
            char *name = NULL;
            int bytes = WideCharToMultiByte(CP_UTF8, 0, data.cFileName, -1, NULL, 0, NULL, NULL);
            if (bytes) name = malloc((size_t)bytes);
            if (!name || !WideCharToMultiByte(CP_UTF8, 0, data.cFileName, -1, name, bytes, NULL, NULL) ||
                addon_join(paths[1], paths[0], name) || discovery_push(&stack, paths[1])) result = -2;
            free(name); if (result) break;
        } while (FindNextFileW(find, &data));
        FindClose(find);
#else
        DIR *dir = opendir(paths[0]); if (!dir) continue;
        struct dirent *entry;
        while ((entry = readdir(dir))) {
            if (cancel && cancel(ctx)) { result = -1; break; }
            if (!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, "..")) continue;
            if (addon_join(paths[1], paths[0], entry->d_name)) { result = -2; break; }
            if (addon_kind(paths[1]) == 2 && discovery_push(&stack, paths[1])) { result = -2; break; }
        }
        closedir(dir);
#endif
        if (result) break;
    }
    while (stack) { SearchNode *n = stack; stack = n->next; free(n->path); free(n); }
    free(paths); return result;
}
#ifdef _WIN32
static int discovery_direct(const char *root, char *out) {
    if (!root || !*root) return 0;
    if (addon_is_forever(root)) { strcpy(out, root); return 1; }
    char *p = malloc(PATH_CAP); if (!p) return 0;
    int found = !addon_join(p, root, "_forever_") && addon_is_forever(p);
    if (found) strcpy(out, p);
    free(p); return found;
}
static int discovery_common(char *out, const char *saved) {
    if (discovery_direct(saved, out)) return 1;
    wchar_t *w = calloc(PATH_CAP, sizeof(*w)); char *path = malloc(PATH_CAP), *base = malloc(PATH_CAP);
    if (!w || !path || !base) { free(w); free(path); free(base); return -2; }
    int found = 0;
    const wchar_t *keys[] = {
        L"SOFTWARE\\WOW6432Node\\Blizzard Entertainment\\World of Warcraft",
        L"SOFTWARE\\Blizzard Entertainment\\World of Warcraft",
        L"SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\World of Warcraft"
    };
    for (size_t i = 0; i < sizeof(keys) / sizeof(*keys) && !found; ++i) {
        DWORD bytes = PATH_CAP * (DWORD)sizeof(*w);
        if (RegGetValueW(HKEY_LOCAL_MACHINE, keys[i], i == 2 ? L"InstallLocation" : L"InstallPath", RRF_RT_REG_SZ, NULL, w, &bytes) == ERROR_SUCCESS &&
            WideCharToMultiByte(CP_UTF8, 0, w, -1, path, PATH_CAP, NULL, NULL)) found = discovery_direct(path, out);
    }
    const wchar_t *envs[] = {L"ProgramFiles(x86)", L"ProgramFiles", L"ProgramW6432"};
    for (size_t i = 0; i < sizeof(envs) / sizeof(*envs) && !found; ++i) {
        DWORD n = GetEnvironmentVariableW(envs[i], w, PATH_CAP);
        if (n && n < PATH_CAP && WideCharToMultiByte(CP_UTF8, 0, w, -1, base, PATH_CAP, NULL, NULL) &&
            !addon_join(path, base, "World of Warcraft")) found = discovery_direct(path, out);
    }
    wchar_t drives[104]; DWORD n = GetLogicalDriveStringsW(104, drives);
    const char *dirs[] = {"World of Warcraft", "Games/World of Warcraft", "Program Files (x86)/World of Warcraft", "Program Files/World of Warcraft", "Blizzard/World of Warcraft", "Battle.net/World of Warcraft"};
    if (n && n < 104) for (wchar_t *drive = drives; *drive && !found; drive += wcslen(drive) + 1) {
        if (!WideCharToMultiByte(CP_UTF8, 0, drive, -1, base, PATH_CAP, NULL, NULL)) continue;
        for (size_t j = 0; j < sizeof(dirs) / sizeof(*dirs) && !found; ++j)
            if (!addon_join(path, base, dirs[j])) found = discovery_direct(path, out);
    }
    free(w); free(path); free(base); return found;
}
/* Mounted volumes include secondary disks without a drive letter. Network
 * mappings are scanned too; CD drives are excluded. */
static int discovery_disks(char *out, DiscoveryCancel cancel, DiscoveryProgress progress, void *ctx) {
    wchar_t volume[PATH_CAP]; HANDLE find = FindFirstVolumeW(volume, PATH_CAP); int result = 0;
    char *root = malloc(PATH_CAP); if (!root) { if (find != INVALID_HANDLE_VALUE) FindVolumeClose(find); return -2; }
    if (find != INVALID_HANDLE_VALUE) {
        do {
            if (cancel && cancel(ctx)) { result = -1; break; }
            UINT type = GetDriveTypeW(volume);
            if (type != DRIVE_FIXED && type != DRIVE_REMOVABLE) continue;
            if (!WideCharToMultiByte(CP_UTF8, 0, volume, -1, root, PATH_CAP, NULL, NULL)) { result = -2; break; }
            result = discovery_tree(root, out, cancel, progress, ctx);
            if (result) break;
        } while (FindNextVolumeW(find, volume, PATH_CAP));
        FindVolumeClose(find);
    }
    wchar_t drives[104]; DWORD n = GetLogicalDriveStringsW(104, drives);
    if (!result && n && n < 104) for (wchar_t *d = drives; *d; d += wcslen(d) + 1) {
        if (GetDriveTypeW(d) != DRIVE_REMOTE) continue;
        if (!WideCharToMultiByte(CP_UTF8, 0, d, -1, root, PATH_CAP, NULL, NULL)) { result = -2; break; }
        result = discovery_tree(root, out, cancel, progress, ctx); if (result) break;
    }
    free(root); return result;
}
#endif
#endif
