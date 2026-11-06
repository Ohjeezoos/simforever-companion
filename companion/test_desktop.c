#define SF_DESKTOP_TEST
#define SF_AUTORUN_KEY L"Software\\Simforever\\CompanionSetupTest"
#define main sf_launcher_main
#define wmain sf_launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
#include <assert.h>
#include <shlwapi.h>
static const char *test_worker, *test_config;
static int test_background;
static const char *test_game;
static DWORD WINAPI host(void *unused) {
    (void)unused; char *args[] = {"test", "-config", (char *)test_config, "-wow", (char *)test_game, NULL};
    return (DWORD)desktop_run(test_worker, 5, args, test_background);
}
static HWND wait_window(void) {
    for (int i = 0; i < 100; ++i) {
        HWND w = FindWindowW(SF_DESKTOP_CLASS, NULL); DWORD pid = 0;
        if (w) GetWindowThreadProcessId(w, &pid);
        if (w && pid == GetCurrentProcessId()) return w;
        Sleep(100);
    }
    return NULL;
}
static unsigned heartbeat(const char *config) {
    char *path = malloc(PATH_CAP); size_t n = 0; assert(path && !addon_join(path, config, "heartbeat"));
    char *text = capture_read(path, &n); unsigned value = text ? (unsigned)strtoul(text, NULL, 10) : 0;
    free(text); free(path); return value;
}
static volatile LONG screenshot_done;
static const char *screenshot_path;
typedef struct { ICoreWebView2CapturePreviewCompletedHandler iface; LONG refs; IStream *stream; } Shot;
static ULONG STDMETHODCALLTYPE shot_add(ICoreWebView2CapturePreviewCompletedHandler *self) { return (ULONG)InterlockedIncrement(&((Shot *)self)->refs); }
static ULONG STDMETHODCALLTYPE shot_release(ICoreWebView2CapturePreviewCompletedHandler *self) {
    Shot *shot = (Shot *)self; LONG n = InterlockedDecrement(&shot->refs);
    if (!n) { IStream_Release(shot->stream); free(shot); } return (ULONG)n;
}
static HRESULT STDMETHODCALLTYPE shot_query(ICoreWebView2CapturePreviewCompletedHandler *self, REFIID iid, void **out) {
    *out = NULL;
    if (!IsEqualIID(iid, &IID_IUnknown) && !IsEqualIID(iid, &IID_ICoreWebView2CapturePreviewCompletedHandler)) return E_NOINTERFACE;
    *out = self; shot_add(self); return S_OK;
}
static HRESULT STDMETHODCALLTYPE shot_complete(ICoreWebView2CapturePreviewCompletedHandler *self, HRESULT error) {
    (void)self; if (FAILED(error)) fprintf(stderr, "Preview failed: %08lx\n", (unsigned long)error);
    InterlockedExchange(&screenshot_done, SUCCEEDED(error) ? 1 : -1); return S_OK;
}
static ICoreWebView2CapturePreviewCompletedHandlerVtbl shot_vtable = {shot_query, shot_add, shot_release, shot_complete};
static void CALLBACK screenshot_timer(HWND window, UINT message, UINT_PTR id, DWORD tick) {
    (void)message; (void)tick; KillTimer(window, id);
    Desktop *d = (Desktop *)GetWindowLongPtrW(window, GWLP_USERDATA); assert(d && d->view && d->view->core);
    Shot *shot = calloc(1, sizeof(*shot)); assert(shot); shot->iface.lpVtbl = &shot_vtable; shot->refs = 1;
    wchar_t *path = wide_path(screenshot_path); assert(path);
    HRESULT rc = SHCreateStreamOnFileEx(path, STGM_CREATE | STGM_WRITE | STGM_SHARE_EXCLUSIVE, FILE_ATTRIBUTE_NORMAL, TRUE, NULL, &shot->stream); free(path);
    assert(SUCCEEDED(rc));
    assert(SUCCEEDED(ICoreWebView2_CapturePreview(d->view->core, COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG, shot->stream, &shot->iface)));
    shot_release(&shot->iface);
}
static void screenshot(HWND window, const char *path) {
    screenshot_path = path; InterlockedExchange(&screenshot_done, 0);
    assert(SetTimer(window, 4321, 300, screenshot_timer));
    for (int i = 0; i < 100 && !InterlockedCompareExchange(&screenshot_done, 0, 0); ++i) Sleep(100);
    assert(InterlockedCompareExchange(&screenshot_done, 0, 0) == 1);
}
static void wait_state(HWND window, unsigned mask, unsigned expected) {
    for (int i = 0; i < 300; ++i) {
        unsigned state = (unsigned)SendMessageW(window, SF_STATE, 0, 0);
        if ((state & mask) == expected) return;
        Sleep(100);
    }
    fprintf(stderr, "state timeout: mask %u expected %u actual %u\n", mask, expected, (unsigned)SendMessageW(window, SF_STATE, 0, 0));
    assert(0);
}
static void script(HWND window, const wchar_t *js) { SendMessageW(window, WM_APP + 6, 0, (LPARAM)js); }
static void quit_host(HWND window, HANDLE thread) {
    SendMessageW(window, WM_COMMAND, SF_QUIT, 0);
    assert(WaitForSingleObject(thread, 15000) == WAIT_OBJECT_0); CloseHandle(thread);
}
static void write_game(const char *game) {
    char *path = malloc(PATH_CAP); assert(path); assert(!mkdir_tree((char *)game));
    assert(!addon_join(path, game, ".flavor.info"));
    const char *flavor = "Product Flavor!STRING:0\nwowf\n"; assert(!capture_atomic(path, flavor, strlen(flavor)));
    assert(!addon_join(path, game, "Wow.exe") && !capture_atomic(path, "fixture", 7)); free(path);
}
static int test_gate_open(const char *url, const char *path, size_t limit) {
    char gate[64]; (void)url; (void)limit; snprintf(gate, sizeof(gate), "SIMFOREVER-ADDON-GATE-1\n%s\n", sf_probe_version);
    return capture_atomic(path, gate, strlen(gate)) ? -1 : 0;
}
int main(int argc, char **argv) {
    _set_error_mode(_OUT_TO_STDERR); assert(argc == 3 || argc == 4); addon_gate_fetch = test_gate_open;
    CoInitializeEx(NULL, COINIT_APARTMENTTHREADED); test_worker = argv[2];
    char (*paths)[PATH_CAP] = calloc(6, sizeof(*paths)); assert(paths);
    test_config = paths[0]; test_game = paths[1];
    assert(!addon_join(paths[0], argv[1], "config") && !mkdir_tree(paths[0]));
    assert(!addon_join(paths[1], argv[1], "Game with spaces/_forever_")); write_game(paths[1]);
    assert(!addon_join(paths[3], argv[1], "appdata") && !mkdir_tree(paths[3]));
    wchar_t *sandbox = wide(paths[3]); assert(sandbox);
    assert(SetEnvironmentVariableW(L"LOCALAPPDATA", sandbox) && SetEnvironmentVariableW(L"APPDATA", sandbox)); free(sandbox);
    cJSON *cfg = cJSON_CreateObject();
    cJSON_AddStringToObject(cfg, "apiBase", "https://127.0.0.1:1"); cJSON_AddStringToObject(cfg, "game", "forever");
    cJSON_AddStringToObject(cfg, "preserveMe", "yes");
    assert(!desktop_json(paths[0], "config.json", cfg)); cJSON_Delete(cfg);
    /* Fresh detection installs and starts sharing without another click. */
    test_background = 0;
    HANDLE thread = CreateThread(NULL, 0, host, NULL, 0, NULL); assert(thread);
    HWND window = wait_window(); assert(window); wait_state(window, 1 | 2 | 4 | 8, 1 | 2 | 4);
    assert(IsWindowVisible(window));
    assert(!addon_join(paths[2], paths[1], "Interface/AddOns/SimforeverProbe") && addon_matches(paths[2]));
    assert(desktop_autorun_enabled());
    assert(!addon_join(paths[4], paths[3], "simforever-companion/app/simforever-companion-" SF_VERSION ".exe") && addon_kind(paths[4]) == 1);
    cfg = capture_config(paths[0]); assert(cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "consentGranted")));
    assert(!strcmp(capture_text(cfg, "preserveMe"), "yes")); cJSON_Delete(cfg);
    wait_state(window, 16, 16); /* Real WebView2 loaded the embedded page and sent ready. */
    if (argc == 4) screenshot(window, argv[3]);
    for (int i = 0; i < 100 && !heartbeat(paths[0]); ++i) Sleep(100);
    assert(heartbeat(paths[0]));
    assert(!addon_join(paths[4], paths[0], "capture-consent.json"));
    size_t size = 0; char *json = capture_read(paths[4], &size); assert(json); cfg = cJSON_Parse(json); free(json);
    assert(cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "approved"))); cJSON_Delete(cfg);
    /* The HTML settings persist independently while the worker is running. */
    script(window, L"document.getElementById('settingsLink').click();document.getElementById('complete').checked=false;document.getElementById('autorun').checked=false;document.querySelector('[data-action=\"settings-save\"]').click()");
    for (int i = 0; i < 100 && desktop_autorun_enabled(); ++i) Sleep(100);
    assert(!desktop_autorun_enabled());
    json = capture_read(paths[4], &size); assert(json); cfg = cJSON_Parse(json); free(json);
    assert(cJSON_IsFalse(cJSON_GetObjectItemCaseSensitive(cfg, "approved"))); cJSON_Delete(cfg);
    script(window, L"document.getElementById('settingsLink').click();document.getElementById('sharing').checked=false;document.querySelector('[data-action=\"settings-save\"]').click()");
    wait_state(window, 2 | 4, 0); unsigned paused = heartbeat(paths[0]); Sleep(400); assert(heartbeat(paths[0]) == paused);
    cfg = capture_config(paths[0]); assert(cJSON_IsFalse(cJSON_GetObjectItemCaseSensitive(cfg, "consentGranted"))); cJSON_Delete(cfg);
    script(window, L"document.getElementById('settingsLink').click();document.getElementById('sharing').checked=true;document.querySelector('[data-action=\"settings-save\"]').click()");
    wait_state(window, 2 | 4, 2 | 4);
    script(window, L"document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}))");
    for (int i = 0; i < 100 && IsWindowVisible(window); ++i) Sleep(50);
    assert(!IsWindowVisible(window)); unsigned before = heartbeat(paths[0]);
    for (int i = 0; i < 100 && heartbeat(paths[0]) <= before; ++i) Sleep(50);
    assert(heartbeat(paths[0]) > before);
    char *args[] = {"test", "-config", paths[0], NULL};
    assert(!desktop_run(test_worker, 3, args, 0));
    for (int i = 0; i < 100 && !IsWindowVisible(window); ++i) Sleep(50);
    assert(IsWindowVisible(window)); quit_host(window, thread);
    before = heartbeat(paths[0]); Sleep(400); assert(heartbeat(paths[0]) == before);
    /* Configured sign-in starts no GUI or WebView; collection still runs. */
    test_background = 1; thread = CreateThread(NULL, 0, host, NULL, 0, NULL); assert(thread); window = wait_window(); assert(window);
    wait_state(window, 1 | 2 | 4, 1 | 2 | 4); assert(!IsWindowVisible(window));
    assert(!((unsigned)SendMessageW(window, SF_STATE, 0, 0) & 16));
    SendMessageW(window, SF_SHOW, 0, 0); assert(IsWindowVisible(window));
    SendMessageW(window, WM_CLOSE, 0, 0); assert(!IsWindowVisible(window)); quit_host(window, thread);
    /* A linked worker owns an isolated config. Account routing must preserve
     * the person's original log preference and the C collector's directory. */
    char secret[65]; memset(secret, 'b', 64); secret[64] = 0;
    cfg = capture_config(paths[0]); cJSON_SetValuestring(cJSON_GetObjectItemCaseSensitive(cfg, "apiBase"), SF_ORIGIN); assert(!desktop_json(paths[0], "config.json", cfg)); cJSON_Delete(cfg);
    assert(!contributor_save(paths[0], secret));
    thread = CreateThread(NULL, 0, host, NULL, 0, NULL); assert(thread); window = wait_window(); assert(window);
    wait_state(window, 1 | 2 | 4, 1 | 2 | 4);
    assert(!addon_join(paths[4], paths[0], "legacy-linked"));
    for (int i = 0; i < 100 && !heartbeat(paths[4]); ++i) Sleep(100);
    assert(heartbeat(paths[4]));
    cfg = capture_config(paths[0]); assert(cJSON_IsFalse(cJSON_GetObjectItemCaseSensitive(cfg, "skipCombatLogs"))); cJSON_Delete(cfg);
    cfg = capture_config(paths[4]); assert(cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "skipCombatLogs"))); cJSON_Delete(cfg);
    quit_host(window, thread);
    assert(!addon_join(paths[4], paths[0], "contributor.dat")); wchar_t *credential = wide_path(paths[4]); assert(credential && DeleteFileW(credential)); free(credential);
    cfg = capture_config(paths[0]); cJSON_SetValuestring(cJSON_GetObjectItemCaseSensitive(cfg, "apiBase"), "https://127.0.0.1:1"); assert(!desktop_json(paths[0], "config.json", cfg)); cJSON_Delete(cfg);
    /* No client found: setup opens, and selecting an ancestor installs its
     * nested Forever client automatically. */
    assert(!addon_join(paths[0], argv[1], "missing/config") && !mkdir_tree(paths[0]));
    cfg = cJSON_CreateObject(); cJSON_AddStringToObject(cfg, "apiBase", "https://127.0.0.1:1");
    cJSON_AddBoolToObject(cfg, "consentGranted", 0); assert(!desktop_json(paths[0], "config.json", cfg)); cJSON_Delete(cfg);
    assert(!addon_join(paths[1], argv[1], "empty") && !mkdir_tree(paths[1]));
    test_background = 0; thread = CreateThread(NULL, 0, host, NULL, 0, NULL); assert(thread); window = wait_window(); assert(window);
    wait_state(window, 8 | 16, 16); assert(!((unsigned)SendMessageW(window, SF_STATE, 0, 0) & 1));
    assert(IsWindowVisible(window));
    assert(!addon_join(paths[5], argv[1], "selected-parent"));
    assert(!addon_join(paths[1], paths[5], "nested/game/_forever_")); write_game(paths[1]);
    SendMessageW(window, WM_APP + 7, 0, (LPARAM)paths[5]); wait_state(window, 1 | 8, 1);
    assert(!addon_join(paths[2], paths[1], "Interface/AddOns/SimforeverProbe") && addon_matches(paths[2]));
    assert(!((unsigned)SendMessageW(window, SF_STATE, 0, 0) & 2)); /* prior refusal retained */
    quit_host(window, thread);
    /* A verified update makes the host quit and start the new release,
     * detached, with its own arguments. The stand-in worker plays the new
     * release: it writes the heartbeat into the -config it is given, which no
     * worker of this unconfigured host does. */
    unsigned restart_before = heartbeat(paths[0]);
    test_background = 1; thread = CreateThread(NULL, 0, host, NULL, 0, NULL); assert(thread); window = wait_window(); assert(window);
    Sleep(300);
    char *found = malloc(strlen(test_worker) + 1); assert(found); strcpy(found, test_worker);
    assert(PostMessageW(window, SF_UPDATE_READY, 0, (LPARAM)found));
    assert(WaitForSingleObject(thread, 15000) == WAIT_OBJECT_0); CloseHandle(thread);
    for (int i = 0; i < 100 && heartbeat(paths[0]) <= restart_before; ++i) Sleep(100);
    assert(heartbeat(paths[0]) > restart_before);
    assert(!addon_join(paths[4], paths[0], "stop") && !capture_atomic(paths[4], "1", 1));
    unsigned stopped = 0; for (int i = 0; i < 50; ++i) { Sleep(200); unsigned now = heartbeat(paths[0]); if (now == stopped) break; stopped = now; }
    Sleep(300); assert(heartbeat(paths[0]) == stopped);
    desktop_restart_path[0] = 0;
    assert(!desktop_autorun(SF_AUTORUN_KEY, "unused", paths[0], 0)); RegDeleteKeyW(HKEY_CURRENT_USER, SF_AUTORUN_KEY);
    CoUninitialize(); free(paths);
    puts("Windows desktop checks passed: automatic install/sharing, actual WebView2 bridge and settings, close/reopen, single instance, silent startup without browser, recursive manual fallback, and restart into a verified update"); return 0;
}
