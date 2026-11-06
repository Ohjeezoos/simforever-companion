/* Native Windows setup and tray host. The upload worker stays headless. */
#ifndef SF_DESKTOP_WINDOWS_H
#define SF_DESKTOP_WINDOWS_H
#include <shellapi.h>
#include <shobjidl.h>
#include <commctrl.h>
#pragma comment(linker, "\"/manifestdependency:type='win32' name='Microsoft.Windows.Common-Controls' version='6.0.0.0' processorArchitecture='*' publicKeyToken='6595b64144ccf1df' language='*'\"")
#include "discovery.h"
#include "webview_windows.h"
#include "sync_windows.h"
#include "contributor_windows.h"
#ifdef SF_DESKTOP_TEST
#define SF_DESKTOP_CLASS L"SimforeverCompanionDesktopTest"
#define SF_DESKTOP_MUTEX L"Local\\SimforeverCompanionDesktopTest"
#else
#define SF_DESKTOP_CLASS L"SimforeverCompanionDesktop035"
#define SF_DESKTOP_MUTEX L"Local\\SimforeverCompanionDesktop"
#endif
#define SF_TRAY (WM_APP + 1)
#define SF_SCAN_DONE (WM_APP + 2)
#define SF_WORKER_DONE (WM_APP + 3)
#define SF_SHOW (WM_APP + 4)
#define SF_STATE (WM_APP + 5)
#define SF_SYNC (WM_APP + 8)
#define SF_ACCOUNT (WM_APP + 9)
#define SF_STATUS_TIMER 1
#define SF_ADDON_TIMER 2
#define SF_ADDON_CHECKED (WM_APP + 10)
/* A running companion's own update: checked every few hours, and once one is
 * verified the host quits and starts it in the background. */
#define SF_UPDATE_TIMER 3
#define SF_UPDATE_READY (WM_APP + 11)
static char desktop_restart_path[PATH_CAP];
static volatile LONG desktop_update_checking;
static DWORD WINAPI desktop_update_thread(void *window) {
    char *cache = malloc(PATH_CAP), *next = malloc(PATH_CAP), *found = NULL;
    if (cache && next && !cache_dir(cache) && update(cache, next, download) > 0) {
        found = malloc(strlen(next) + 1); if (found) strcpy(found, next);
    }
    free(cache); free(next);
    if (!PostMessageW((HWND)window, SF_UPDATE_READY, 0, (LPARAM)found)) { free(found); InterlockedExchange(&desktop_update_checking, 0); }
    return 0;
}
/* The new release, detached, with this run's arguments and the hand-off. */
static int desktop_start_release(const char *path, int argc, char **argv) {
    char **args = restart_args(path, argc, argv); if (!args) return -1;
    wchar_t *exe = wide(path), *command = calloc(PATH_CAP * 4, sizeof(wchar_t)); size_t used = 0, cap = PATH_CAP * 4; int ok = exe && command;
    for (int i = 0; ok && args[i]; ++i) {
        wchar_t *arg = wide(args[i]); if (!arg) { ok = 0; break; }
        if (used + 3 >= cap) { free(arg); ok = 0; break; }
        if (i) command[used++] = ' ';
        command[used++] = '"'; size_t slashes = 0;
        for (wchar_t *q = arg;; ++q) {
            if (*q == '\\') { ++slashes; continue; }
            size_t n = *q == '"' || !*q ? slashes * 2 : slashes;
            if (*q == '"') ++n;
            if (used + n + 3 >= cap) { ok = 0; break; }
            while (n--) command[used++] = '\\';
            slashes = 0;
            if (!*q) break;
            command[used++] = *q;
        }
        if (ok) command[used++] = '"';
        free(arg);
    }
    STARTUPINFOW si = {0}; PROCESS_INFORMATION pi = {0}; si.cb = sizeof(si);
    if (ok) { command[used] = 0; ok = CreateProcessW(exe, command, NULL, NULL, FALSE, 0, NULL, NULL, &si, &pi); }
    if (ok) { CloseHandle(pi.hThread); CloseHandle(pi.hProcess); }
    free(exe); free(command); free(args); return ok ? 0 : -1;
}
#define SF_BROWSE 101
#define SF_DEEP 102
#define SF_CANCEL 103
#define SF_INSTALL 104
#define SF_AUTO 105
#define SF_SHARE 106
#define SF_COMPLETE 107
#define SF_QUIT 108
#define SF_OPEN 109
#define SF_CLOSE 110
#define SF_SETTINGS 111
#define SF_DONE 112

typedef struct {
    HWND window, status, folder, browse, deep, cancel, install, notice, settings;
    int autorun, share, complete, installed, fallback, automatic_share;
    char headline[160], subtitle[512], kind[16];
    SfView *view;
    HFONT font, heading; HBRUSH background;
    HANDLE scan, scan_cancel, worker, sync_thread, mutex, link_thread, link_cancel;
    int linked, link_revoked;
    char account_name[256], account_status[256], account_code[16];
    double account_points;
    CRITICAL_SECTION progress_lock;
    char config[PATH_CAP], game[PATH_CAP], scan_root[PATH_CAP], found[PATH_CAP], progress[PATH_CAP];
    char worker_path[PATH_CAP], self[PATH_CAP];
    int argc; char **argv;
    int scan_mode, scan_result, ready, busy, background_start, tray_added, stopping;
    ULONGLONG progress_at;
    UINT taskbar_created;
    int addon_held; volatile LONG addon_checking;
} Desktop;

static void desktop_render(Desktop *d);
static void desktop_stop_worker(Desktop *d);
static void desktop_start(Desktop *d);
static void desktop_account_post(Desktop *d, cJSON *model, int linked) {
    char *json = cJSON_PrintUnformatted(model); cJSON_Delete(model);
    if (json && !PostMessageW(d->window, SF_ACCOUNT, (WPARAM)linked, (LPARAM)json)) free(json);
}
static DWORD WINAPI desktop_link_thread(void *context) {
    Desktop *d = context; long status = 0;
    cJSON *reply = contributor_request("health", NULL, NULL, 0, &status);
    int configured = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(reply, "signInConfigured")); cJSON_Delete(reply);
    char secret[65] = {0}, code[16] = {0};
    if (status != 200 || !configured) goto unavailable;
    reply = contributor_request("device/start", NULL, NULL, 0, &status);
    const char *key = capture_text(reply, "deviceToken"), *user_code = capture_text(reply, "userCode");
    if (status != 201 || !contributor_secret(key) || !user_code || strlen(user_code) != 8 || strspn(user_code, "ABCDEFGHJKLMNPQRSTUVWXYZ23456789") != 8) { cJSON_Delete(reply); goto unavailable; }
    memcpy(secret, key, 65); strcpy(code, user_code); cJSON_Delete(reply);
    cJSON *model = cJSON_CreateObject(); cJSON_AddStringToObject(model, "code", code); cJSON_AddStringToObject(model, "status", "Confirm this code in your browser."); desktop_account_post(d, model, 0);
    char url[2048]; snprintf(url, sizeof(url), "%s/contribute/?code=%s", SF_ORIGIN, code); wchar_t *w = wide(url);
    if (w) { ShellExecuteW(NULL, L"open", w, NULL, NULL, SW_SHOWNORMAL); free(w); }
    for (int i = 0; i < 120; ++i) {
        if (WaitForSingleObject(d->link_cancel, 5000) == WAIT_OBJECT_0) { SecureZeroMemory(secret, sizeof(secret)); return 0; }
        reply = contributor_request("device/status", secret, NULL, 0, &status);
        const char *state = capture_text(reply, "status");
        if (status == 200 && state && !strcmp(state, "linked")) {
            if (contributor_save(d->config, secret)) { cJSON_Delete(reply); goto unavailable; }
            cJSON_AddStringToObject(reply, "message", "Accepted new logs earn points automatically."); desktop_account_post(d, reply, 1);
            SecureZeroMemory(secret, sizeof(secret)); return 0;
        }
        cJSON_Delete(reply); if (status == 401) break;
    }
    model = cJSON_CreateObject(); cJSON_AddStringToObject(model, "status", "Linking expired. Choose Link account to try again."); desktop_account_post(d, model, 0);
    SecureZeroMemory(secret, sizeof(secret)); return 0;
unavailable:
    SecureZeroMemory(secret, sizeof(secret));
    { cJSON *error = cJSON_CreateObject(); cJSON_AddStringToObject(error, "status", "Account linking is unavailable. Your recordings still sync without points."); desktop_account_post(d, error, 0); }
    return 1;
}
static void desktop_link_account(Desktop *d) {
    if (!d->installed) return;
    if (!contributor_endpoint(d->config)) { strcpy(d->account_status, "Account linking uses the Simforever upload server. Your custom upload target is unchanged."); desktop_render(d); return; }
    if (d->linked && !d->link_revoked) { wchar_t *url = wide(SF_ORIGIN "/contribute/"); if (url) { ShellExecuteW(d->window, L"open", url, NULL, NULL, SW_SHOWNORMAL); free(url); } return; }
    if (d->link_thread) {
        if (WaitForSingleObject(d->link_thread, 0) == WAIT_TIMEOUT) return;
        CloseHandle(d->link_thread); d->link_thread = NULL;
    }
    if (!d->link_cancel) d->link_cancel = CreateEventW(NULL, TRUE, FALSE, NULL);
    if (!d->link_cancel) return;
    ResetEvent(d->link_cancel); strcpy(d->account_status, "Opening account linking in your browser."); d->account_code[0] = 0; desktop_render(d);
    d->link_thread = CreateThread(NULL, 0, desktop_link_thread, d, 0, NULL);
}
static void desktop_page_message(void *context, const char *message);
static void desktop_status(Desktop *d, const wchar_t *text) {
    WideCharToMultiByte(CP_UTF8, 0, text, -1, d->subtitle, sizeof(d->subtitle), NULL, NULL);
    if (d->status) SetWindowTextW(d->status, text);
    desktop_render(d);
}
static void desktop_show(Desktop *d) {
    if (!d->view && !d->fallback) {
        char *profile = malloc(PATH_CAP);
        if (profile && !addon_join(profile, d->config, "webview") && !mkdir_tree(profile))
            d->view = sf_view_create(d->window, profile, desktop_page_message, d);
        free(profile);
        if (!d->view) { d->fallback = 1; desktop_page_message(d, "webview-error"); }
    }
    sf_view_visible(d->view, 1); ShowWindow(d->window, SW_SHOWNORMAL); SetForegroundWindow(d->window);
}
static void desktop_hide(Desktop *d) {
    ShowWindow(d->window, SW_HIDE);
    sf_view_close(d->view); d->view = NULL;
}
static void desktop_error(Desktop *d, const wchar_t *text) {
#ifdef SF_DESKTOP_TEST
    (void)d; fwprintf(stderr, L"Setup error: %s\n", text); abort();
#else
    MessageBoxW(d->window, text, L"Simforever Companion", MB_OK | MB_ICONERROR);
#endif
}
static int desktop_json(const char *dir, const char *name, cJSON *obj) {
    char *path = malloc(PATH_CAP), *json = cJSON_PrintUnformatted(obj); int rc = -1;
    if (path && json && !addon_join(path, dir, name)) rc = capture_atomic(path, json, strlen(json));
    free(path); free(json); return rc;
}
static int desktop_configured(Desktop *d) {
    cJSON *cfg = capture_config(d->config);
    const char *path = capture_text(cfg, "wowPath");
    int ok = path && strlen(path) < PATH_CAP && addon_is_forever(path);
    cJSON *choice = cJSON_GetObjectItemCaseSensitive(cfg, "consentGranted");
    d->automatic_share = !cJSON_IsBool(choice);
    d->share = d->automatic_share || cJSON_IsTrue(choice);
    if (ok) strcpy(d->game, path);
    cJSON_Delete(cfg); return ok;
}
#ifndef SF_AUTORUN_KEY
#define SF_AUTORUN_KEY L"Software\\Microsoft\\Windows\\CurrentVersion\\Run"
#endif
static const wchar_t desktop_run_key[] = SF_AUTORUN_KEY;
static int desktop_link(const char *executable, const char *config_dir, const char *relative);
static int desktop_appdata_path(char *out, const char *relative) {
    wchar_t *base = calloc(PATH_CAP, sizeof(*base)); char *utf = malloc(PATH_CAP); int result = -1;
    if (base && utf) {
        DWORD n = GetEnvironmentVariableW(L"APPDATA", base, PATH_CAP);
        if (n && n < PATH_CAP && WideCharToMultiByte(CP_UTF8, 0, base, -1, utf, PATH_CAP, NULL, NULL)) result = addon_join(out, utf, relative);
    }
    free(base); free(utf); return result;
}
static const char desktop_startup_link[] = "Microsoft/Windows/Start Menu/Programs/Startup/Simforever Companion.lnk";
static int desktop_autorun(const wchar_t *subkey, const char *executable, const char *config, int enable) {
    /* Startup shortcuts support long executable/config paths. Run registry
     * values have a 260-character command limit. Remove the previous entry. */
    HKEY key;
    if (RegOpenKeyExW(HKEY_CURRENT_USER, subkey, 0, KEY_SET_VALUE, &key) == ERROR_SUCCESS) {
        LONG rc = RegDeleteValueW(key, L"SimforeverCompanion"); RegCloseKey(key);
        if (rc != ERROR_SUCCESS && rc != ERROR_FILE_NOT_FOUND) return -1;
    }
    if (enable) return desktop_link(executable, config, desktop_startup_link);
    char *path = malloc(PATH_CAP); if (!path) return -1;
    int rc = desktop_appdata_path(path, desktop_startup_link);
    if (!rc) { remove_file(path); rc = addon_kind(path) == 0 ? 0 : -1; }
    free(path); return rc;
}
static int desktop_autorun_enabled(void) {
    char *path = malloc(PATH_CAP); if (!path) return 0;
    int exists = !desktop_appdata_path(path, desktop_startup_link) && addon_kind(path) == 1; free(path);
    DWORD bytes = 0;
    return exists || RegGetValueW(HKEY_CURRENT_USER, desktop_run_key, L"SimforeverCompanion", RRF_RT_REG_SZ, NULL, NULL, &bytes) == ERROR_SUCCESS;
}
/* Versioned copies avoid overwriting a running executable. The normal update
 * verifier still checks and hands off to newer cached releases on each start. */
static int desktop_install_app(Desktop *d, char *out) {
    wchar_t *base = calloc(PATH_CAP, sizeof(*base)); char *utf = malloc(PATH_CAP), *dir = malloc(PATH_CAP);
    if (!base || !utf || !dir) { free(base); free(utf); free(dir); return -1; }
    DWORD n = GetEnvironmentVariableW(L"LOCALAPPDATA", base, PATH_CAP); int rc = -1;
    if (!n || n >= PATH_CAP || !WideCharToMultiByte(CP_UTF8, 0, base, -1, utf, PATH_CAP, NULL, NULL) ||
        addon_join(dir, utf, "simforever-companion/app") || mkdir_tree(dir) ||
        addon_join(out, dir, "simforever-companion-" SF_VERSION ".exe")) goto done;
    FILE *source = open_file(d->self, "rb"); Release release = {0}; unsigned char hash[32];
    if (!source) goto done;
    if (file_size(source, &release.size) || sha256(source, release.size, hash)) { fclose(source); goto done; }
    fclose(source); hex_digest(hash, release.hash);
    if (!verify(out, &release)) { rc = 0; goto done; }
    if (addon_kind(out) != 0) goto done;
    wchar_t *a = wide_path(d->self), *b = wide_path(out);
    if (a && b && CopyFileW(a, b, TRUE) && !verify(out, &release)) rc = 0;
    free(a); free(b);
done:
    if (!rc) {
        if (addon_join(utf, dir, "THIRD_PARTY_NOTICES.txt") ||
            capture_atomic(utf, (const char *)sf_ui_notices, sizeof(sf_ui_notices) - 1)) rc = -1;
    }
    free(base); free(utf); free(dir); return rc;
}
static int desktop_link(const char *executable, const char *config_dir, const char *relative) {
    wchar_t *exe = wide(executable), *config = wide(config_dir);
    char *path = malloc(PATH_CAP); int result = -1;
    if (!exe || !config || !path || desktop_appdata_path(path, relative)) goto done;
    char *last = strrchr(path, '/');
    if (!last) goto done;
    *last = 0; int made = mkdir_tree(path); *last = '/'; if (made) goto done;
    IShellLinkW *link = NULL;
    if (SUCCEEDED(CoCreateInstance(&CLSID_ShellLink, NULL, CLSCTX_INPROC_SERVER, &IID_IShellLinkW, (void **)&link))) {
        IPersistFile *file = NULL; wchar_t *target = wide_path(path);
        size_t len = wcslen(config); while (len && (config[len - 1] == '\\' || config[len - 1] == '/')) config[--len] = 0;
        wchar_t *args = calloc(wcslen(config) + 40, sizeof(*args));
        if (args && !wcschr(config, L'"')) {
            int startup = !strcmp(relative, desktop_startup_link);
            swprintf(args, wcslen(config) + 40, startup ? L"--background -config \"%s\"" : L"-config \"%s\"", config);
            if (SUCCEEDED(IShellLinkW_SetPath(link, exe)) && SUCCEEDED(IShellLinkW_SetArguments(link, args)) &&
                SUCCEEDED(IShellLinkW_SetDescription(link, L"WoW Forever combat recording companion")) &&
                SUCCEEDED(IShellLinkW_QueryInterface(link, &IID_IPersistFile, (void **)&file))) {
                if (target && SUCCEEDED(IPersistFile_Save(file, target, TRUE))) result = 0;
                IPersistFile_Release(file);
            }
        }
        free(args); free(target); IShellLinkW_Release(link);
    }
done:
    free(exe); free(config); free(path); return result;
}
static int desktop_save(Desktop *d, int grant) {
    cJSON *cfg = capture_config(d->config);
    char *config_path = malloc(PATH_CAP);
    if (!config_path || addon_join(config_path, d->config, "config.json")) { free(config_path); cJSON_Delete(cfg); return -1; }
    /* A malformed existing file is not permission to replace its preferences. */
    if (!cfg && addon_kind(config_path) != 0) { free(config_path); return -1; }
    free(config_path);
    if (!cfg) cfg = cJSON_CreateObject();
    if (!cfg) return -1;
    const char *prior = capture_text(cfg, "wowPath");
    int changed = !prior || strcmp(prior, d->game);
    cJSON_DeleteItemFromObjectCaseSensitive(cfg, "wowPath"); cJSON_AddStringToObject(cfg, "wowPath", d->game);
    if (!capture_text(cfg, "apiBase")) cJSON_AddStringToObject(cfg, "apiBase", SF_ORIGIN);
    cJSON_DeleteItemFromObjectCaseSensitive(cfg, "game"); cJSON_AddStringToObject(cfg, "game", "forever");
    cJSON_DeleteItemFromObjectCaseSensitive(cfg, "consentGranted"); cJSON_AddBoolToObject(cfg, "consentGranted", d->share);
    if (!cJSON_HasObjectItem(cfg, "skipCombatLogs")) cJSON_AddBoolToObject(cfg, "skipCombatLogs", 0);
    if (changed) {
        cJSON_DeleteItemFromObjectCaseSensitive(cfg, "watch"); cJSON_AddArrayToObject(cfg, "watch");
        cJSON_DeleteItemFromObjectCaseSensitive(cfg, "watchLogs"); cJSON_AddArrayToObject(cfg, "watchLogs");
    }
    char url[4096]; int rc = capture_url(cfg, url);
    char *consent_path = malloc(PATH_CAP);
    int have_consent = consent_path && !addon_join(consent_path, d->config, "capture-consent.json") && addon_kind(consent_path) != 0;
    free(consent_path);
    if (!rc && (grant || !have_consent)) {
        cJSON *consent = cJSON_CreateObject();
        cJSON_AddStringToObject(consent, "endpoint", url);
        cJSON_AddBoolToObject(consent, "approved", grant && d->share && d->complete);
        cJSON_AddStringToObject(consent, "notice", "complete-recordings-1");
        cJSON_AddStringToObject(consent, "source", d->automatic_share ? "automatic-setup" : "settings");
        cJSON_AddNumberToObject(consent, "at", (double)time(NULL));
        rc = desktop_json(d->config, "capture-consent.json", consent); cJSON_Delete(consent);
    }
    if (!rc) rc = desktop_json(d->config, "config.json", cfg);
    cJSON_Delete(cfg);
    if (!rc) {
        cJSON *settings = cJSON_CreateObject();
        cJSON_AddBoolToObject(settings, "completeRecordings", d->complete);
        cJSON_AddBoolToObject(settings, "autorun", d->autorun);
        rc = desktop_json(d->config, "desktop-settings.json", settings); cJSON_Delete(settings);
    }
    return rc;
}
static int desktop_scan_cancel(void *ctx) { return WaitForSingleObject(((Desktop *)ctx)->scan_cancel, 0) == WAIT_OBJECT_0; }
static void desktop_progress(void *ctx, const char *path) {
    Desktop *d = ctx; ULONGLONG now = GetTickCount64();
    if (now - d->progress_at < 150) return;
    EnterCriticalSection(&d->progress_lock); strcpy(d->progress, path); LeaveCriticalSection(&d->progress_lock); d->progress_at = now;
}
static DWORD WINAPI desktop_scan_thread(void *ctx) {
    Desktop *d = ctx;
    if (d->scan_mode == 0) {
#ifdef SF_DESKTOP_TEST
        /* Keep unit setup inside its explicit fixture root. */
        d->scan_result = discovery_direct(d->scan_root, d->found);
#else
        d->scan_result = discovery_common(d->found, d->scan_root);
#endif
    }
    else if (d->scan_mode == 1) d->scan_result = discovery_tree(d->scan_root, d->found, desktop_scan_cancel, desktop_progress, d);
    else d->scan_result = discovery_disks(d->found, desktop_scan_cancel, desktop_progress, d);
    PostMessageW(d->window, SF_SCAN_DONE, 0, 0); return 0;
}
static void desktop_search(Desktop *d, int mode) {
    if (d->busy || d->installed) return;
    d->scan_mode = mode; d->scan_result = 0; d->found[0] = 0; d->progress_at = 0;
    ResetEvent(d->scan_cancel); d->busy = 1;
    strcpy(d->headline, "Looking for WoW Forever"); d->kind[0] = 0;
    EnableWindow(d->browse, FALSE); EnableWindow(d->deep, FALSE); EnableWindow(d->install, FALSE); EnableWindow(d->cancel, TRUE); ShowWindow(d->cancel, mode ? SW_SHOW : SW_HIDE);
    desktop_status(d, mode == 2 ? L"Searching all accessible disks. This can take a while." : L"Looking for WoW Forever...");
    d->scan = CreateThread(NULL, 0, desktop_scan_thread, d, 0, NULL);
    if (!d->scan) { d->busy = 0; PostMessageW(d->window, SF_SCAN_DONE, 0, 0); }
}
static void desktop_browse(Desktop *d) {
    IFileOpenDialog *dialog = NULL;
    HRESULT rc = CoCreateInstance(&CLSID_FileOpenDialog, NULL, CLSCTX_INPROC_SERVER, &IID_IFileOpenDialog, (void **)&dialog);
    if (FAILED(rc)) { desktop_error(d, L"The folder picker could not open. Please restart the companion."); return; }
    DWORD flags = 0; IFileOpenDialog_GetOptions(dialog, &flags);
    IFileOpenDialog_SetOptions(dialog, flags | FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST);
    IFileOpenDialog_SetTitle(dialog, L"Choose the WoW folder or a folder containing it");
    if (SUCCEEDED(IFileOpenDialog_Show(dialog, d->window))) {
        IShellItem *item = NULL; PWSTR path = NULL;
        if (SUCCEEDED(IFileOpenDialog_GetResult(dialog, &item))) {
            if (SUCCEEDED(IShellItem_GetDisplayName(item, SIGDN_FILESYSPATH, &path))) {
                if (WideCharToMultiByte(CP_UTF8, 0, path, -1, d->scan_root, PATH_CAP, NULL, NULL)) desktop_search(d, 1);
                CoTaskMemFree(path);
            }
            IShellItem_Release(item);
        }
    }
    IFileOpenDialog_Release(dialog);
}
static HICON desktop_icon(void) {
    HICON icon = LoadIconW(GetModuleHandleW(NULL), MAKEINTRESOURCEW(301));
    return icon ? icon : LoadIconW(NULL, MAKEINTRESOURCEW(32512));
}
static void desktop_tray(Desktop *d, DWORD action) {
    NOTIFYICONDATAW icon = {0}; icon.cbSize = sizeof(icon); icon.hWnd = d->window; icon.uID = 1;
    icon.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP; icon.uCallbackMessage = SF_TRAY;
    icon.hIcon = desktop_icon();
    wcscpy(icon.szTip, L"Simforever Companion");
    if (Shell_NotifyIconW(action, &icon) && action == NIM_ADD) {
        icon.uVersion = NOTIFYICON_VERSION_4; Shell_NotifyIconW(NIM_SETVERSION, &icon); d->tray_added = 1;
    }
}
static DWORD WINAPI desktop_worker_thread(void *ctx) {
    Desktop *d = ctx;
    char *worker_config = NULL;
    if (d->linked) {
        worker_config = malloc(PATH_CAP); cJSON *cfg = capture_config(d->config);
        int rc = !worker_config || !cfg || addon_join(worker_config, d->config, "legacy-linked") || mkdir_tree(worker_config);
        if (!rc) {
            cJSON_DeleteItemFromObjectCaseSensitive(cfg, "skipCombatLogs"); cJSON_AddBoolToObject(cfg, "skipCombatLogs", 1);
            rc = desktop_json(worker_config, "config.json", cfg);
        }
        cJSON_Delete(cfg);
        if (rc) { free(worker_config); PostMessageW(d->window, SF_WORKER_DONE, 1, 0); return 1; }
        desktop_worker_config = worker_config;
    }
    char **args = calloc((size_t)d->argc + 8, sizeof(*args));
    if (!args) { desktop_worker_config = NULL; free(worker_config); PostMessageW(d->window, SF_WORKER_DONE, 1, 0); return 1; }
    for (int i = 0; i < d->argc; ++i) args[i] = d->argv[i];
    int n = d->argc;
    args[n++] = "-console"; args[n++] = "-config"; args[n++] = d->config;
    args[n++] = "-wow"; args[n++] = d->game;
    /* C sends combat logs on every server, so the legacy worker never does. */
    args[n++] = "-logs=false";
    int code = 0;
    while (WaitForSingleObject(desktop_stop, 0) == WAIT_TIMEOUT) {
        code = launch(d->worker_path, n, args, 0);
        if (WaitForSingleObject(desktop_stop, 10000) == WAIT_OBJECT_0) break;
    }
    free(args); desktop_worker_config = NULL; free(worker_config); PostMessageW(d->window, SF_WORKER_DONE, (WPARAM)code, 0); return (DWORD)code;
}
/* Whether combat logs are shared. The worker saves the -logs=false this host
 * gives it as `skipCombatLogs`, so that key is the worker's and not the
 * player's: 0.41.0 and 0.42.0 read it and so sent no anonymous logs. A
 * -logs=false the player gives on the command line still keeps them local, as
 * does `shareCombatLogs: false` in config.json. */
static int desktop_logs_wanted(Desktop *d, cJSON *prefs) {
    for (int i = 1; i < d->argc; ++i) {
        const char *a = d->argv[i]; if (*a != '-') continue; ++a; if (*a == '-') ++a;
        if (!strcmp(a, "logs=false") || !strcmp(a, "logs=0") || (!strcmp(a, "logs") && i + 1 < d->argc && (!strcmp(d->argv[i + 1], "false") || !strcmp(d->argv[i + 1], "0")))) return 0;
    }
    cJSON *share = cJSON_GetObjectItemCaseSensitive(prefs, "shareCombatLogs");
    return !share || cJSON_IsTrue(share);
}
static DWORD WINAPI desktop_sync_thread(void *context) {
    Desktop *d = context; SyncCache cache = {0}; ContributionLoop contributions = {0}; int previous = -1;
    do {
        int c_logs = 1;
        int state = sync_state_mode(&cache, d->config, d->linked, c_logs);
        char secret[65] = {0};
        if (d->linked && contributor_endpoint(d->config) && contributor_load(d->config, secret)) {
            if (GetTickCount64() >= contributions.next_poll) {
                long status = 0; cJSON *account = contributor_request("device/status", secret, NULL, 0, &status);
                const char *linked = capture_text(account, "status");
                if (status == 200 && linked && !strcmp(linked, "linked")) desktop_account_post(d, account, 2);
                else { cJSON_Delete(account); if (status == 401) { cJSON *notice = cJSON_CreateObject(); cJSON_AddStringToObject(notice, "status", "Account link expired or was revoked. Link again to resume credited uploads."); desktop_account_post(d, notice, 3); } }
                contributions.next_poll = GetTickCount64() + 60000;
            }
            cJSON *prefs = capture_config(d->config);
            if (desktop_logs_wanted(d, prefs)) {
                int logs = contributor_logs(&contributions, d->config, d->game, secret, NULL, NULL);
                if (logs > 0) state = SF_SYNC_PENDING; else if (!logs && state == SF_SYNC_READY) state = SF_SYNC_DONE;
            }
            cJSON_Delete(prefs);
            SecureZeroMemory(secret, sizeof(secret));
        } else if (!d->linked) {
            cJSON *prefs = capture_config(d->config);
            char custom[4096], display[128]; int simforever = contributor_endpoint(d->config);
            if (cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(prefs, "consentGranted")) && desktop_logs_wanted(d, prefs) &&
                (simforever || !contributor_custom_url(d->config, custom, display))) {
                int logs = contributor_logs(&contributions, d->config, d->game, NULL, simforever ? NULL : custom, simforever ? NULL : display);
                if (logs > 0) state = SF_SYNC_PENDING; else if (!logs && state == SF_SYNC_READY) state = SF_SYNC_DONE;
            }
            cJSON_Delete(prefs);
        }
        if (WaitForSingleObject(desktop_stop, 0) == WAIT_OBJECT_0) break;
        if (state != previous) { PostMessageW(d->window, SF_SYNC, (WPARAM)state, 0); previous = state; }
    } while (WaitForSingleObject(desktop_stop, 5000) == WAIT_TIMEOUT);
    sync_cache_free(&cache); contributor_loop_free(&contributions); return 0;
}
static void desktop_start(Desktop *d) {
    if (d->worker || !d->installed || !d->share) return;
    d->ready = 1; strcpy(d->headline, "Ready to sync"); strcpy(d->kind, "good");
    desktop_status(d, L"Your recordings will sync when the game saves them. You can close this window.");
    d->worker = CreateThread(NULL, 0, desktop_worker_thread, d, 0, NULL);
    if (d->worker) d->sync_thread = CreateThread(NULL, 0, desktop_sync_thread, d, 0, NULL);
    if (!d->worker) { d->ready = 0; strcpy(d->kind, "bad"); desktop_status(d, L"The background task could not start. Quit and reopen the companion."); }
}
static int desktop_install(Desktop *d) {
    if (d->busy || d->installed) return 0;
    if (!addon_is_forever(d->game)) { desktop_error(d, L"Choose a WoW Forever installation. Retail and Classic are not supported."); return -1; }
    char *app = malloc(PATH_CAP); if (!app) return -1;
    strcpy(d->headline, "Installing the companion"); d->kind[0] = 0;
    desktop_status(d, L"Installing Simforever Probe in WoW Forever...");
    if ((!no_addon && addon_install_cleared(d->game, &d->addon_held) < 0) || desktop_install_app(d, app) || mkdir_tree(d->config) || desktop_save(d, d->automatic_share)) {
        strcpy(d->headline, "Installation needs attention"); strcpy(d->kind, "bad");
        desktop_status(d, L"The installation could not finish. Check that the game folder is writable, then choose it again."); free(app); return -1;
    }
    int shortcuts = desktop_link(app, d->config, "Microsoft/Windows/Start Menu/Programs/Simforever Companion.lnk");
    int startup = desktop_autorun(desktop_run_key, app, d->config, d->autorun); free(app);
    d->installed = 1; strcpy(d->headline, d->share ? "Ready to sync" : "Sharing paused"); strcpy(d->kind, "good");
    desktop_status(d, d->share ? L"New recordings sync automatically." : L"Recordings stay on this computer. Resume sharing in settings.");
    if (d->addon_held) {
        desktop_status(d, L"The Simforever addon installs itself here once it is cleared for this game version. Nothing else is needed.");
        SetTimer(d->window, SF_ADDON_TIMER, SF_ADDON_GATE_RECHECK * 1000, NULL);
    }
    if (shortcuts || startup) desktop_error(d, L"The addon is installed, but a Windows shortcut could not be saved. Keep the downloaded companion to open it manually.");
    desktop_start(d); return 0;
}
/* The check owns its copy of the folder, so quitting mid-check is safe. */
typedef struct { HWND window; char game[PATH_CAP]; } DesktopAddonCheck;
static DWORD WINAPI desktop_addon_thread(LPVOID param) {
    DesktopAddonCheck *c = param; int held = 1;
    int rc = addon_install_cleared(c->game, &held);
    PostMessageW(c->window, SF_ADDON_CHECKED, (WPARAM)held, (LPARAM)rc);
    free(c); return 0;
}
static HWND desktop_control(Desktop *d, const wchar_t *kind, const wchar_t *label, DWORD style, int x, int y, int w, int h, int id) {
    HWND control = CreateWindowExW(!wcscmp(kind, L"EDIT") ? WS_EX_CLIENTEDGE : 0, kind, label,
        WS_CHILD | WS_VISIBLE | style, x, y, w, h, d->window, (HMENU)(INT_PTR)id, GetModuleHandleW(NULL), NULL);
    SendMessageW(control, WM_SETFONT, (WPARAM)d->font, TRUE); return control;
}
static void desktop_controls(Desktop *d) {
    HWND title = desktop_control(d, L"STATIC", L"Simforever Companion", 0, 32, 24, 610, 36, 0);
    SendMessageW(title, WM_SETFONT, (WPARAM)d->heading, TRUE);
    d->status = desktop_control(d, L"STATIC", L"", 0, 32, 132, 610, 110, 0);
    d->folder = desktop_control(d, L"STATIC", L"", SS_PATHELLIPSIS | SS_NOPREFIX, 32, 270, 610, 28, 0);
    d->browse = desktop_control(d, L"BUTTON", L"Choose folder", WS_TABSTOP, 32, 328, 170, 36, SF_BROWSE);
    d->deep = desktop_control(d, L"BUTTON", L"Search all disks", WS_TABSTOP, 218, 328, 170, 36, SF_DEEP);
    d->cancel = desktop_control(d, L"BUTTON", L"Cancel search", WS_TABSTOP, 404, 328, 170, 36, SF_CANCEL);
    d->notice = desktop_control(d, L"STATIC", L"Sharing sends combat logs and addon recordings to Simforever, including recorded player names and gear.", 0, 32, 400, 610, 50, 0);
}
static void desktop_render(Desktop *d) {
    if (!d->view || !d->view->ready) return;
    cJSON *model = cJSON_CreateObject();
    cJSON_AddStringToObject(model, "version", SF_VERSION);
    cJSON_AddStringToObject(model, "headline", d->headline);
    cJSON_AddStringToObject(model, "sub", d->subtitle);
    cJSON_AddStringToObject(model, "kind", d->kind);
    cJSON_AddStringToObject(model, "path", d->busy && d->scan_mode ? d->progress : d->game);
    cJSON_AddBoolToObject(model, "installed", d->installed);
    cJSON_AddBoolToObject(model, "busy", d->busy);
    cJSON_AddBoolToObject(model, "sharing", d->share);
    cJSON_AddBoolToObject(model, "complete", d->complete);
    cJSON_AddBoolToObject(model, "autorun", d->autorun);
    cJSON_AddBoolToObject(model, "linked", d->linked && !d->link_revoked);
    cJSON_AddStringToObject(model, "accountName", d->account_name);
    cJSON_AddStringToObject(model, "accountStatus", d->account_status);
    cJSON_AddStringToObject(model, "accountCode", d->account_code);
    cJSON_AddNumberToObject(model, "points", d->account_points);
    char *json = cJSON_PrintUnformatted(model); cJSON_Delete(model);
    if (json) (void)sf_view_post(d->view, json); free(json);
}
static void desktop_stop_worker(Desktop *d) {
    d->ready = 0; SetEvent(desktop_stop); if (desktop_job) TerminateJobObject(desktop_job, 0);
    if (d->worker) { WaitForSingleObject(d->worker, INFINITE); CloseHandle(d->worker); d->worker = NULL; }
    if (d->sync_thread) { WaitForSingleObject(d->sync_thread, INFINITE); CloseHandle(d->sync_thread); d->sync_thread = NULL; }
    MSG message;
    while (PeekMessageW(&message, d->window, SF_WORKER_DONE, SF_WORKER_DONE, PM_REMOVE)) { }
    while (PeekMessageW(&message, d->window, SF_SYNC, SF_SYNC, PM_REMOVE)) { }
    ResetEvent(desktop_stop);
}
static void desktop_settings_save(Desktop *d, int share, int complete, int autorun) {
    int old_share = d->share;
    if (share != old_share && d->worker) desktop_stop_worker(d);
    d->share = share; d->automatic_share = 0;
    int old_complete = d->complete, old_autorun = d->autorun;
    d->complete = complete; d->autorun = autorun;
    int rc = 0;
    if (d->installed && share != old_share) rc = desktop_save(d, 1);
    if (d->installed && !rc) {
        /* The worker owns config.json while running. Its capture consent and
         * desktop preferences are separate atomic files. */
        cJSON *cfg = capture_config(d->config); char url[4096]; rc = capture_url(cfg, url); cJSON_Delete(cfg);
        if (!rc) {
            cJSON *consent = cJSON_CreateObject(); cJSON_AddStringToObject(consent, "endpoint", url);
            cJSON_AddBoolToObject(consent, "approved", d->share && complete);
            cJSON_AddStringToObject(consent, "notice", "complete-recordings-1");
            rc = desktop_json(d->config, "capture-consent.json", consent); cJSON_Delete(consent);
        }
        cJSON *prefs = cJSON_CreateObject(); cJSON_AddBoolToObject(prefs, "completeRecordings", complete); cJSON_AddBoolToObject(prefs, "autorun", autorun);
        if (!rc) rc = desktop_json(d->config, "desktop-settings.json", prefs); cJSON_Delete(prefs);
        char *app = malloc(PATH_CAP);
        if (!rc) rc = !app || desktop_install_app(d, app) || desktop_autorun(desktop_run_key, app, d->config, autorun);
        free(app);
    }
    if (rc) { cJSON *saved = capture_config(d->config); d->share = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(saved, "consentGranted")); cJSON_Delete(saved); d->complete = old_complete; d->autorun = old_autorun; desktop_error(d, L"The settings could not be saved. Please try again."); }
    if (d->installed && d->share) desktop_start(d);
    else if (d->installed) { strcpy(d->headline, "Sharing paused"); strcpy(d->kind, "still"); desktop_status(d, L"Recordings stay on this computer. Resume sharing in settings."); }
    desktop_render(d);
}
static void desktop_page_message(void *context, const char *message) {
    Desktop *d = context;
    if (!strcmp(message, "ready")) { desktop_render(d); return; }
    if (!strcmp(message, "webview-error")) {
        d->fallback = 1;
        if (!d->status) desktop_controls(d);
        desktop_status(d, L"The web interface could not open. You can still select the game folder here. Microsoft Edge WebView2 Runtime restores the full interface.");
        return;
    }
    if (!strcmp(message, "close")) { desktop_hide(d); return; }
    if (!strcmp(message, "link-account")) { desktop_link_account(d); return; }
    if (!strcmp(message, "browse")) { if (!d->busy && !d->installed) desktop_browse(d); return; }
    if (!strcmp(message, "deep")) { desktop_search(d, 2); return; }
    if (!strcmp(message, "cancel")) { SetEvent(d->scan_cancel); if (d->scan) CancelSynchronousIo(d->scan); return; }
    cJSON *request = cJSON_Parse(message);
    const char *action = capture_text(request, "action");
    cJSON *share = cJSON_GetObjectItemCaseSensitive(request, "sharing"), *complete = cJSON_GetObjectItemCaseSensitive(request, "complete"), *autorun = cJSON_GetObjectItemCaseSensitive(request, "autorun");
    if (action && !strcmp(action, "settings-save") && cJSON_IsBool(share) && cJSON_IsBool(complete) && cJSON_IsBool(autorun))
        desktop_settings_save(d, cJSON_IsTrue(share), cJSON_IsTrue(complete), cJSON_IsTrue(autorun));
    cJSON_Delete(request);
}
static void desktop_quit(Desktop *d) {
    if (d->stopping) return;
    d->stopping = 1; SetEvent(d->scan_cancel); if (d->scan) CancelSynchronousIo(d->scan); SetEvent(desktop_stop);
    if (desktop_job) TerminateJobObject(desktop_job, 0);
    ShowWindow(d->window, SW_HIDE);
    if (d->scan) { WaitForSingleObject(d->scan, INFINITE); CloseHandle(d->scan); d->scan = NULL; }
    if (d->worker) { WaitForSingleObject(d->worker, INFINITE); CloseHandle(d->worker); d->worker = NULL; }
    if (d->sync_thread) { WaitForSingleObject(d->sync_thread, INFINITE); CloseHandle(d->sync_thread); d->sync_thread = NULL; }
    if (d->link_cancel) SetEvent(d->link_cancel);
    if (d->link_thread) { WaitForSingleObject(d->link_thread, INFINITE); CloseHandle(d->link_thread); d->link_thread = NULL; }
    if (d->link_cancel) { CloseHandle(d->link_cancel); d->link_cancel = NULL; }
    MSG queued; while (PeekMessageW(&queued, d->window, SF_ACCOUNT, SF_ACCOUNT, PM_REMOVE)) free((char *)queued.lParam);
    DestroyWindow(d->window);
}
static LRESULT CALLBACK desktop_proc(HWND window, UINT message, WPARAM wp, LPARAM lp) {
    Desktop *d = (Desktop *)GetWindowLongPtrW(window, GWLP_USERDATA);
    if (message == WM_NCCREATE) {
        d = ((CREATESTRUCTW *)lp)->lpCreateParams; d->window = window;
        SetWindowLongPtrW(window, GWLP_USERDATA, (LONG_PTR)d);
    }
    if (!d) return DefWindowProcW(window, message, wp, lp);
    if (message == d->taskbar_created && d->taskbar_created) { desktop_tray(d, NIM_ADD); return 0; }
    switch (message) {
    case SF_ACCOUNT: {
        char *raw = (char *)lp; cJSON *account = cJSON_Parse(raw); free(raw);
        const char *name = capture_text(account, "name"), *status = capture_text(account, "status"), *code = capture_text(account, "code");
        if (wp == 1 || wp == 2) {
            if (name) snprintf(d->account_name, sizeof(d->account_name), "%s", name);
            cJSON *points = cJSON_GetObjectItemCaseSensitive(account, "points"); if (cJSON_IsNumber(points)) d->account_points = points->valuedouble;
            strcpy(d->account_status, "Accepted new logs earn points automatically."); d->account_code[0] = 0;
            if (wp == 1) { desktop_stop_worker(d); d->linked = 1; d->link_revoked = 0; desktop_start(d); }
        } else {
            if (wp == 3) d->link_revoked = 1;
            if (status) snprintf(d->account_status, sizeof(d->account_status), "%s", status);
            snprintf(d->account_code, sizeof(d->account_code), "%s", code ? code : "");
        }
        cJSON_Delete(account); desktop_render(d); return 0;
    }
    case WM_CTLCOLORSTATIC: case WM_CTLCOLORBTN:
        SetBkColor((HDC)wp, RGB(10, 27, 44)); SetTextColor((HDC)wp, RGB(223, 230, 238)); return (LRESULT)d->background;
    case WM_DRAWITEM: {
        DRAWITEMSTRUCT *draw = (DRAWITEMSTRUCT *)lp; if (draw->CtlID != SF_SHARE) break;
        HBRUSH brush = CreateSolidBrush(RGB(192, 138, 69)); FillRect(draw->hDC, &draw->rcItem, brush); DeleteObject(brush);
        SetBkMode(draw->hDC, TRANSPARENT); SetTextColor(draw->hDC, RGB(27, 18, 6));
        DrawTextW(draw->hDC, L"Start sharing", -1, &draw->rcItem, DT_CENTER | DT_VCENTER | DT_SINGLELINE); return TRUE;
    }
    case WM_CLOSE: desktop_hide(d); return 0;
    case WM_SETFOCUS:
        if (d->view && d->view->controller) ICoreWebView2Controller_MoveFocus(d->view->controller, COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC);
        return 0;
    case WM_SIZE:
        if (wp == SIZE_MINIMIZED) desktop_hide(d); else sf_view_bounds(d->view); return 0;
    case SF_SHOW: desktop_show(d); return 0;
    case SF_STATE: return d->installed | (d->share << 1) | (d->ready << 2) | (d->busy << 3) | ((d->view && d->view->ready) << 4);
#ifdef SF_DESKTOP_TEST
    case WM_APP + 6:
        if (d->view && d->view->core) ICoreWebView2_ExecuteScript(d->view->core, (const wchar_t *)lp, NULL);
        return 0;
    case WM_APP + 7:
        strcpy(d->scan_root, (const char *)lp); desktop_search(d, 1); return 0;
#endif
    case WM_QUERYENDSESSION: return TRUE;
    case WM_ENDSESSION: if (wp) desktop_quit(d); return 0;
    case SF_TRAY:
        if (LOWORD(lp) == WM_LBUTTONDBLCLK || LOWORD(lp) == NIN_SELECT || LOWORD(lp) == NIN_KEYSELECT) desktop_show(d);
        if (LOWORD(lp) == WM_CONTEXTMENU || LOWORD(lp) == WM_RBUTTONUP) {
            HMENU menu = CreatePopupMenu(); AppendMenuW(menu, MF_STRING, SF_OPEN, L"Open Simforever Companion");
            AppendMenuW(menu, MF_STRING | (desktop_autorun_enabled() ? MF_CHECKED : 0), SF_AUTO, L"Start with Windows");
            AppendMenuW(menu, MF_SEPARATOR, 0, NULL); AppendMenuW(menu, MF_STRING, SF_QUIT, L"Quit");
            POINT point; GetCursorPos(&point); SetForegroundWindow(window);
            UINT selected = TrackPopupMenu(menu, TPM_RETURNCMD | TPM_RIGHTBUTTON, point.x, point.y, 0, window, NULL);
            DestroyMenu(menu); if (selected) SendMessageW(window, WM_COMMAND, selected, 0); PostMessageW(window, WM_NULL, 0, 0);
        } return 0;
    case WM_COMMAND:
        switch (LOWORD(wp)) {
        case SF_OPEN: desktop_show(d); break;
        case SF_CLOSE: desktop_hide(d); break;
        case SF_QUIT: desktop_quit(d); break;
        case SF_BROWSE: desktop_page_message(d, "browse"); break;
        case SF_DEEP: desktop_page_message(d, "deep"); break;
        case SF_CANCEL: desktop_page_message(d, "cancel"); break;
        case SF_AUTO: desktop_settings_save(d, d->share, d->complete, !desktop_autorun_enabled()); break;
        }
        return 0;
    case SF_SCAN_DONE:
        if (d->scan) { WaitForSingleObject(d->scan, INFINITE); CloseHandle(d->scan); d->scan = NULL; }
        d->busy = 0;
        if (d->scan_result == 1 && !desktop_scan_cancel(d)) {
            strcpy(d->game, d->found); (void)desktop_install(d);
        } else {
            strcpy(d->headline, "Choose your WoW Forever folder"); strcpy(d->kind, "still");
            desktop_status(d, d->scan_result == -2 ? L"The search could not finish. Try choosing a smaller folder." :
                desktop_scan_cancel(d) ? L"Search cancelled. Choose a folder or search again." :
                L"Select the game folder, or search all accessible disks. Retail, Classic and beta clients are excluded.");
        }
        desktop_render(d);
        if (!d->background_start) desktop_show(d);
        return 0;
    case SF_SYNC:
        if (!d->ready) return 0;
        strcpy(d->kind, "good");
        if (wp == SF_SYNC_DONE) { strcpy(d->headline, "Synced"); desktop_status(d, L"New recordings will sync automatically. You can close this window."); }
        else if (wp == SF_SYNC_PENDING) { strcpy(d->headline, "Syncing"); d->kind[0] = 0; desktop_status(d, L"Recordings are uploading automatically. You can close this window."); }
        else { strcpy(d->headline, "Ready to sync"); desktop_status(d, L"Play WoW Forever as usual. New recordings sync automatically."); }
        return 0;
    case SF_WORKER_DONE:
        if (!d->stopping && d->ready) { strcpy(d->headline, "Tracking stopped"); strcpy(d->kind, "bad"); desktop_status(d, L"Quit and reopen the companion to restart tracking."); }
        return 0;
    case SF_ADDON_CHECKED:
        InterlockedExchange(&d->addon_checking, 0);
        if (!wp && (int)lp >= 0) {
            d->addon_held = 0; KillTimer(window, SF_ADDON_TIMER);
            if (d->ready) desktop_status(d, L"Simforever Probe is installed. Log in or reload the game interface to load it.");
        }
        return 0;
    case SF_UPDATE_READY: {
        /* Restart only between tasks: never mid-setup or while linking. */
        char *found = (char *)lp; InterlockedExchange(&desktop_update_checking, 0);
        if (found && !d->stopping && !d->busy && !d->link_thread && strlen(found) < PATH_CAP) {
            strcpy(desktop_restart_path, found); desktop_quit(d);
        }
        free(found); return 0;
    }
    case WM_TIMER:
        if (wp == SF_UPDATE_TIMER) {
            if (!InterlockedCompareExchange(&desktop_update_checking, 1, 0)) {
                HANDLE t = CreateThread(NULL, 0, desktop_update_thread, window, 0, NULL);
                if (t) CloseHandle(t); else InterlockedExchange(&desktop_update_checking, 0);
            }
            return 0;
        }
        if (wp == SF_ADDON_TIMER) {
            if (d->addon_held && !no_addon && !InterlockedCompareExchange(&d->addon_checking, 1, 0)) {
                DesktopAddonCheck *c = malloc(sizeof(*c));
                HANDLE t = NULL;
                if (c) { c->window = window; strcpy(c->game, d->game); t = CreateThread(NULL, 0, desktop_addon_thread, c, 0, NULL); }
                if (t) CloseHandle(t); else { free(c); InterlockedExchange(&d->addon_checking, 0); }
            }
            return 0;
        }
        if (d->busy && d->scan_mode) { EnterCriticalSection(&d->progress_lock); desktop_render(d); LeaveCriticalSection(&d->progress_lock); }
        return 0;
    case WM_DESTROY:
        sf_view_close(d->view); d->view = NULL;
        desktop_tray(d, NIM_DELETE); KillTimer(window, SF_STATUS_TIMER); KillTimer(window, SF_ADDON_TIMER); KillTimer(window, SF_UPDATE_TIMER); PostQuitMessage(0); return 0;
    }
    return DefWindowProcW(window, message, wp, lp);
}
static int desktop_run(const char *worker_path, int argc, char **argv, int background) {
    Desktop *d = calloc(1, sizeof(*d)); AddonManager *args = calloc(1, sizeof(*args));
    if (!d || !args) { free(d); free(args); return 1; }
    if (!addon_init(args, argc, argv) || self_path(d->self)) { free(args); free(d); return 1; }
    strcpy(d->config, args->config); strcpy(d->scan_root, args->explicit_wow); free(args);
    if (!d->scan_root[0]) {
        cJSON *cfg = capture_config(d->config); const char *saved = capture_text(cfg, "wowPath");
        if (saved && strlen(saved) < PATH_CAP) strcpy(d->scan_root, saved);
        cJSON_Delete(cfg);
    }
    strcpy(d->worker_path, worker_path); d->argc = argc; d->argv = argv; d->background_start = background;
    { char secret[65] = {0}; d->linked = contributor_endpoint(d->config) && contributor_load(d->config, secret); SecureZeroMemory(secret, sizeof(secret)); }
    /* All desktop instances share the same tray host. This also prevents two
     * independently configured workers fighting over a game installation. */
    d->mutex = CreateMutexW(NULL, FALSE, SF_DESKTOP_MUTEX);
    if (!d->mutex) { free(d); return 1; }
    if (GetLastError() == ERROR_ALREADY_EXISTS) {
        HWND existing = FindWindowW(SF_DESKTOP_CLASS, NULL);
        for (int retry = 0; !existing && !background && retry < 60; ++retry) { Sleep(50); existing = FindWindowW(SF_DESKTOP_CLASS, NULL); }
        if (existing && !background) PostMessageW(existing, SF_SHOW, 0, 0);
        CloseHandle(d->mutex); free(d); return 0;
    }
    InitializeCriticalSection(&d->progress_lock);
    d->scan_cancel = CreateEventW(NULL, TRUE, FALSE, NULL); desktop_stop = CreateEventW(NULL, TRUE, FALSE, NULL);
    desktop_job = CreateJobObjectW(NULL, NULL);
    if (!d->scan_cancel || !desktop_stop || !desktop_job) {
        if (d->scan_cancel) CloseHandle(d->scan_cancel); if (desktop_stop) CloseHandle(desktop_stop);
        if (desktop_job) CloseHandle(desktop_job); CloseHandle(d->mutex); DeleteCriticalSection(&d->progress_lock); free(d); return 1;
    }
    JOBOBJECT_EXTENDED_LIMIT_INFORMATION limits = {0}; limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
    if (!SetInformationJobObject(desktop_job, JobObjectExtendedLimitInformation, &limits, sizeof(limits))) {
        CloseHandle(desktop_job); CloseHandle(desktop_stop); CloseHandle(d->scan_cancel); CloseHandle(d->mutex);
        DeleteCriticalSection(&d->progress_lock); free(d); return 1;
    }
    CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
    INITCOMMONCONTROLSEX common = {sizeof(common), ICC_STANDARD_CLASSES}; InitCommonControlsEx(&common);
    d->background = CreateSolidBrush(RGB(10, 27, 44));
    d->font = CreateFontW(-16, 0, 0, 0, FW_NORMAL, 0, 0, 0, DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, 0, L"Segoe UI");
    d->heading = CreateFontW(-26, 0, 0, 0, FW_SEMIBOLD, 0, 0, 0, DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, 0, L"Segoe UI");
    WNDCLASSW wc = {0}; wc.lpfnWndProc = desktop_proc; wc.hInstance = GetModuleHandleW(NULL);
    wc.lpszClassName = SF_DESKTOP_CLASS; wc.hCursor = LoadCursorW(NULL, MAKEINTRESOURCEW(32512)); wc.hIcon = desktop_icon();
    wc.hbrBackground = d->background; RegisterClassW(&wc);
    d->taskbar_created = RegisterWindowMessageW(L"TaskbarCreated");
    RECT rect = {0, 0, 680, 540}; AdjustWindowRectEx(&rect, WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX, FALSE, 0);
    d->window = CreateWindowExW(0, SF_DESKTOP_CLASS, L"Simforever Companion", WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX,
        CW_USEDEFAULT, CW_USEDEFAULT, rect.right - rect.left, rect.bottom - rect.top, NULL, NULL, wc.hInstance, d);
    int result = 1;
    if (d->window) {
        /* Match the original dark title bar as well as its HTML colors. */
        HMODULE dwm = LoadLibraryExW(L"dwmapi.dll", NULL, LOAD_LIBRARY_SEARCH_SYSTEM32);
        if (dwm) {
            typedef HRESULT (WINAPI *SetDwmAttribute)(HWND, DWORD, LPCVOID, DWORD);
            SetDwmAttribute attribute = (SetDwmAttribute)GetProcAddress(dwm, "DwmSetWindowAttribute"); BOOL dark = TRUE;
            if (attribute) attribute(d->window, 20, &dark, sizeof(dark)); FreeLibrary(dwm);
        }
        desktop_tray(d, NIM_ADD); SetTimer(d->window, SF_STATUS_TIMER, 250, NULL);
        if (!no_update_check) SetTimer(d->window, SF_UPDATE_TIMER, SF_UPDATE_EVERY_SECONDS * 1000u, NULL);
        int configured = desktop_configured(d);
        d->complete = 1; d->autorun = desktop_autorun_enabled() || !configured;
        char *prefs_path = malloc(PATH_CAP); size_t prefs_size = 0;
        char *prefs_text = prefs_path && !addon_join(prefs_path, d->config, "desktop-settings.json") ? capture_read(prefs_path, &prefs_size) : NULL;
        cJSON *prefs = prefs_text ? cJSON_ParseWithLengthOpts(prefs_text, prefs_size + 1, NULL, 1) : NULL;
        if (cJSON_IsBool(cJSON_GetObjectItemCaseSensitive(prefs, "completeRecordings")))
            d->complete = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(prefs, "completeRecordings"));
        if (cJSON_IsBool(cJSON_GetObjectItemCaseSensitive(prefs, "autorun")))
            d->autorun = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(prefs, "autorun"));
        if (d->share && !prefs && !d->automatic_share) {
            size_t n = 0; char *text = prefs_path && !addon_join(prefs_path, d->config, "capture-consent.json") ? capture_read(prefs_path, &n) : NULL;
            cJSON *consent = text ? cJSON_ParseWithLengthOpts(text, n + 1, NULL, 1) : NULL;
            d->complete = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(consent, "approved")); cJSON_Delete(consent); free(text);
        }
        cJSON_Delete(prefs); free(prefs_text); free(prefs_path);
        if (configured) {
            (void)desktop_install(d);
            if (!background) desktop_show(d);
        } else if (!background) desktop_search(d, 0);
        else { strcpy(d->headline, "Setup needs attention"); desktop_status(d, L"Choose your WoW Forever folder to start tracking."); }
        MSG msg;
        while (GetMessageW(&msg, NULL, 0, 0) > 0) {
            if ((d->view && d->view->ready) || !IsDialogMessageW(d->window, &msg)) { TranslateMessage(&msg); DispatchMessageW(&msg); }
        }
        result = 0;
    }
    if (desktop_job) CloseHandle(desktop_job); desktop_job = NULL;
    CloseHandle(desktop_stop); desktop_stop = NULL; CloseHandle(d->scan_cancel); CloseHandle(d->mutex);
    /* The single-instance lock is released, so the update can take over. */
    if (desktop_restart_path[0] && desktop_start_release(desktop_restart_path, d->argc, d->argv))
        result = 1;
    DeleteObject(d->font); DeleteObject(d->heading); DeleteObject(d->background);
    DeleteCriticalSection(&d->progress_lock); CoUninitialize(); free(d); return result;
}
#endif
