/* Account linking stays in C. Device credentials never reach the webview,
 * command line, the legacy worker or its shared config.json. */
#ifndef SF_CONTRIBUTOR_WINDOWS_H
#define SF_CONTRIBUTOR_WINDOWS_H
#include <wincrypt.h>
#include <fcntl.h>
#pragma comment(lib, "crypt32.lib")
static int contributor_endpoint(const char *dir) {
    cJSON *cfg = capture_config(dir); const char *api = capture_text(cfg, "apiBase");
    size_t n = api ? strlen(api) : 0; while (n && api[n-1] == '/') --n;
    int ok = !api || !*api || (n == strlen(SF_ORIGIN) && !memcmp(api, SF_ORIGIN, n));
    cJSON_Delete(cfg); return ok;
}
static int contributor_secret(const char *s) {
    if (!s || strlen(s) != 64) return 0;
    for (int i = 0; i < 64; ++i) if (!strchr("0123456789abcdef", s[i])) return 0;
    return 1;
}
static int contributor_save(const char *dir, const char *secret) {
    if (!contributor_secret(secret)) return -1;
    cJSON *j = cJSON_CreateObject(); cJSON_AddStringToObject(j, "origin", SF_ORIGIN); cJSON_AddStringToObject(j, "token", secret);
    char *raw = cJSON_PrintUnformatted(j); cJSON_Delete(j); if (!raw) return -1;
    DATA_BLOB in = {(DWORD)strlen(raw), (BYTE *)raw}, out = {0}; int rc = -1;
    if (CryptProtectData(&in, L"Simforever contributor device", NULL, NULL, NULL, CRYPTPROTECT_UI_FORBIDDEN, &out)) {
        char *path = malloc(PATH_CAP);
        if (path && !addon_join(path, dir, "contributor.dat")) rc = capture_atomic(path, (const char *)out.pbData, out.cbData);
        free(path); LocalFree(out.pbData);
    }
    SecureZeroMemory(raw, strlen(raw)); free(raw); return rc;
}
static int contributor_load(const char *dir, char secret[65]) {
    char *path = malloc(PATH_CAP); size_t n = 0;
    char *raw = path && !addon_join(path, dir, "contributor.dat") ? capture_read(path, &n) : NULL; free(path);
    if (!raw || n > 16384) { free(raw); return 0; }
    DATA_BLOB in = {(DWORD)n, (BYTE *)raw}, out = {0}; int ok = 0;
    if (CryptUnprotectData(&in, NULL, NULL, NULL, NULL, CRYPTPROTECT_UI_FORBIDDEN, &out)) {
        cJSON *j = cJSON_ParseWithLengthOpts((const char *)out.pbData, out.cbData, NULL, 0);
        const char *token = capture_text(j, "token"), *endpoint = capture_text(j, "origin");
        if (endpoint && !strcmp(endpoint, SF_ORIGIN) && contributor_secret(token)) { memcpy(secret, token, 65); ok = 1; }
        cJSON_Delete(j); SecureZeroMemory(out.pbData, out.cbData); LocalFree(out.pbData);
    }
    SecureZeroMemory(raw, n); free(raw); return ok;
}
static cJSON *contributor_request(const char *path, const char *token, FILE *file, DWORD size, long *status) {
    *status = 0; char url[2048];
    if (token && !contributor_secret(token)) return NULL;
#ifdef SF_DESKTOP_TEST
    /* Native desktop fixtures must never contact the public contribution service. */
    (void)path; (void)file; (void)size; return NULL;
#endif
    if (snprintf(url, sizeof(url), "%s/contributor-api/%s", SF_ORIGIN, path) >= (int)sizeof(url)) return NULL;
    wchar_t *w = wide(url); URL_COMPONENTS u = {0}; u.dwStructSize = sizeof(u); u.dwHostNameLength = u.dwUrlPathLength = (DWORD)-1;
    HINTERNET session = NULL, conn = NULL, req = NULL; cJSON *result = NULL; CaptureBuffer response = {0};
    if (!w || !WinHttpCrackUrl(w, 0, 0, &u) || u.nScheme != INTERNET_SCHEME_HTTPS || u.dwHostNameLength >= 256 || (token && !contributor_secret(token))) goto done;
    wchar_t host[256]; memcpy(host, u.lpszHostName, u.dwHostNameLength * sizeof(wchar_t)); host[u.dwHostNameLength] = 0;
    session = WinHttpOpen(L"Simforever-Contributor/1", WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY, NULL, NULL, 0);
    if (!session || !WinHttpSetTimeouts(session, 5000, 5000, 15000, 15000)) goto done;
    conn = WinHttpConnect(session, host, u.nPort, 0); if (!conn) goto done;
    const wchar_t *method = file || !strcmp(path, "device/start") ? L"POST" : L"GET";
    req = WinHttpOpenRequest(conn, method, u.lpszUrlPath, NULL, NULL, NULL, WINHTTP_FLAG_SECURE);
    DWORD redirects = WINHTTP_OPTION_REDIRECT_POLICY_NEVER;
    if (!req || !WinHttpSetOption(req, WINHTTP_OPTION_REDIRECT_POLICY, &redirects, sizeof(redirects))) goto done;
    wchar_t headers[256]; wcscpy(headers, file ? L"Content-Type: application/octet-stream\r\n" : L"Content-Type: application/json\r\n");
    if (token) { wchar_t *auth = wide(token); if (!auth) goto done; wcscat(headers, L"Authorization: Bearer "); wcscat(headers, auth); wcscat(headers, L"\r\n"); free(auth); }
    if (!WinHttpSendRequest(req, headers, (DWORD)-1, NULL, 0, size, 0)) goto done;
    ULONGLONG deadline = GetTickCount64() + 120000;
    if (file) {
        rewind(file); char buf[65536]; DWORD left = size;
        while (left) {
            if (GetTickCount64() > deadline || (desktop_stop && WaitForSingleObject(desktop_stop, 0) == WAIT_OBJECT_0)) goto done;
            DWORD n = left > sizeof(buf) ? sizeof(buf) : left, wrote = 0;
            if (fread(buf, 1, n, file) != n || !WinHttpWriteData(req, buf, n, &wrote) || wrote != n) goto done;
            left -= n;
        }
    }
    if (!WinHttpReceiveResponse(req, NULL)) goto done;
    DWORD code = 0, length = sizeof(code); if (!WinHttpQueryHeaders(req, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER, NULL, &code, &length, NULL)) goto done;
    *status = (long)code;
    for (;;) {
        char buf[4096]; DWORD n = 0;
        if (GetTickCount64() > deadline || !WinHttpReadData(req, buf, sizeof(buf), &n)) goto done;
        if (!n) break;
        if (response.n + n > 65536 || cb_add(&response, buf, n)) goto done;
    }
    if (response.s) result = cJSON_ParseWithLengthOpts(response.s, response.n + 1, NULL, 1);
done:
    if (req) WinHttpCloseHandle(req); if (conn) WinHttpCloseHandle(conn); if (session) WinHttpCloseHandle(session);
    free(w); free(response.s); return result;
}
static int contributor_receipt(const char *dir, const char *secret, const char *digest, char *path) {
    char owner[65], name[160]; if (capture_hash_bytes(secret, strlen(secret), owner)) return -1;
    snprintf(name, sizeof(name), "contribution-%s-%s.json", owner, digest); return addon_join(path, dir, name);
}
typedef struct { ULONGLONG next_poll, next_upload; SyncCache files; } ContributionLoop;
static FILE *contributor_spool(const char *dir) {
    unsigned char random[16]; char name[80] = "contribution-spool-";
    if (BCryptGenRandom(NULL, random, sizeof(random), BCRYPT_USE_SYSTEM_PREFERRED_RNG)) return NULL;
    size_t start = strlen(name); for (size_t i = 0; i < sizeof(random); ++i) snprintf(name + start + i * 2, 3, "%02x", random[i]);
    strcat(name, ".tmp"); char *path = malloc(PATH_CAP);
    wchar_t *w = path && !addon_join(path, dir, name) ? wide_path(path) : NULL; free(path);
    HANDLE file = w ? CreateFileW(w, GENERIC_READ | GENERIC_WRITE, 0, NULL, CREATE_NEW, FILE_ATTRIBUTE_TEMPORARY | FILE_FLAG_DELETE_ON_CLOSE, NULL) : INVALID_HANDLE_VALUE; free(w);
    if (file == INVALID_HANDLE_VALUE) return NULL;
    int fd = _open_osfhandle((intptr_t)file, _O_BINARY | _O_RDWR); if (fd < 0) { CloseHandle(file); return NULL; }
    FILE *result = _fdopen(fd, "w+b"); if (!result) _close(fd); return result;
}
static int contributor_logs(ContributionLoop *loop, const char *dir, const char *game, const char *secret) {
    char *pattern = malloc(PATH_CAP), *path = malloc(PATH_CAP), *receipt = malloc(PATH_CAP); int pending = 0, sent = 0, seen = 0;
    if (!pattern || !path || !receipt || addon_join(pattern, game, "Logs/WoWCombatLog*.txt")) { free(pattern); free(path); free(receipt); return 1; }
    wchar_t *w = wide_path(pattern); WIN32_FIND_DATAW data = {0}; HANDLE find = w ? FindFirstFileW(w, &data) : INVALID_HANDLE_VALUE; free(w);
    if (find != INVALID_HANDLE_VALUE) {
        FILETIME clock; GetSystemTimeAsFileTime(&clock); uint64_t now = ((uint64_t)clock.dwHighDateTime << 32) | clock.dwLowDateTime;
        do {
            if (data.dwFileAttributes & (FILE_ATTRIBUTE_DIRECTORY | FILE_ATTRIBUTE_REPARSE_POINT)) continue;
            char name[1024]; if (!WideCharToMultiByte(CP_UTF8, 0, data.cFileName, -1, name, sizeof(name), NULL, NULL)) { ++pending; continue; }
            uint64_t modified = ((uint64_t)data.ftLastWriteTime.dwHighDateTime << 32) | data.ftLastWriteTime.dwLowDateTime;
            uint64_t size = ((uint64_t)data.nFileSizeHigh << 32) | data.nFileSizeLow;
            if (!size) continue;
            ++seen;
            /* A session log is sent after 15 quiet minutes, not after every pause between pulls;
             * the account service forwards a re-sent log's new lines only. */
            if (size > 256u * 1024u * 1024u || modified > now || now - modified < UINT64_C(9000000000)) { ++pending; continue; }
            if (addon_join(pattern, game, "Logs") || addon_join(path, pattern, name)) { ++pending; continue; }
            const char *digest = sync_hash(&loop->files, path);
            if (!digest || contributor_receipt(dir, secret, digest, receipt)) { ++pending; continue; }
            if (addon_kind(receipt) == 1) continue;
            ++pending;
            if (sent || GetTickCount64() < loop->next_upload || (desktop_stop && WaitForSingleObject(desktop_stop, 0) == WAIT_OBJECT_0)) continue;
            FILE *input = open_file(path, "rb"), *spool = contributor_spool(dir);
            if (!input || !spool) { if (input) fclose(input); if (spool) fclose(spool); continue; }
            uint64_t left = size; char buf[65536]; int ok = 1;
            while (left) { size_t n = left > sizeof(buf) ? sizeof(buf) : (size_t)left; if (fread(buf, 1, n, input) != n || fwrite(buf, 1, n, spool) != n) { ok = 0; break; } left -= n; }
            fclose(input); unsigned char bytes[32]; char copied[65]; rewind(spool);
            if (ok && !sha256(spool, size, bytes)) { hex_digest(bytes, copied); ok = !strcmp(copied, digest); } else ok = 0;
            if (ok) {
                long status = 0; cJSON *reply = contributor_request("logs", secret, spool, (DWORD)size, &status); const char *hash = capture_text(reply, "sha256");
                if (reply && status >= 200 && status < 300 && hash && !strcmp(hash, digest)) {
                    char *json = cJSON_PrintUnformatted(reply);
                    if (json && !capture_atomic(receipt, json, strlen(json))) --pending;
                    free(json);
                }
                cJSON_Delete(reply); sent = 1; loop->next_upload = GetTickCount64() + (status == 429 ? 600000 : status == 401 ? 60000 : 15000);
            }
            fclose(spool);
        } while (FindNextFileW(find, &data));
        FindClose(find);
    }
    free(pattern); free(path); free(receipt); return pending ? 1 : seen ? 0 : -1;
}
#endif
