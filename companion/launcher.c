#if defined(__APPLE__) && !defined(_DARWIN_C_SOURCE)
#define _DARWIN_C_SOURCE 1
#endif
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <errno.h>
#include <limits.h>
#include <sys/stat.h>
#ifdef _WIN32
#define COBJMACROS
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <winhttp.h>
#include <bcrypt.h>
#include <io.h>
#else
#include <unistd.h>
#include <fcntl.h>
#include <sys/file.h>
#include <curl/curl.h>
#ifdef __APPLE__
#include <mach-o/dyld.h>
#include <mach-o/getsect.h>
#include <mach-o/loader.h>
#include <CommonCrypto/CommonDigest.h>
extern const struct mach_header_64 _mh_execute_header;
#else
#include <openssl/evp.h>
#endif
#endif

#ifndef SF_VERSION
#define SF_VERSION "0.38.0"
#endif
#ifndef SF_TARGET
#define SF_TARGET "linux-amd64"
#endif
#ifndef SF_ORIGIN
#define SF_ORIGIN "https://forever.howtoguides.online"
#endif
#ifdef _WIN32
#define EXT ".exe"
#else
#define EXT ""
#endif
#define PATH_CAP 32768
#define MAX_BINARY (128u * 1024u * 1024u)
#define FOOTER_SIZE 46

typedef struct { char version[32], hash[65]; uint64_t size; } Release;
typedef struct { FILE *file; size_t total, limit; } Sink;

#ifdef _WIN32
static wchar_t *wide(const char *s) {
    int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s, -1, NULL, 0);
    wchar_t *out = n ? calloc((size_t)n, sizeof(*out)) : NULL;
    if (out && !MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s, -1, out, n)) {
        free(out); return NULL;
    }
    return out;
}
static wchar_t *wide_path(const char *s) {
    wchar_t *w = wide(s); if (!w) return NULL;
    for (wchar_t *p = w; *p; ++p) if (*p == '/') *p = '\\';
    if (!wcsncmp(w, L"\\\\?\\", 4)) return w;
    DWORD n = GetFullPathNameW(w, 0, NULL, NULL);
    wchar_t *full = n && n < PATH_CAP ? calloc((size_t)n, sizeof(*full)) : NULL;
    if (!full || !GetFullPathNameW(w, n, full, NULL)) { free(w); free(full); return NULL; }
    free(w);
    for (wchar_t *p = full; *p; ++p) if (*p == '/') *p = '\\';
    int unc = full[0] == '\\' && full[1] == '\\';
    size_t len = wcslen(full), prefix = unc ? 8u : 4u;
    if (len + prefix >= PATH_CAP) { free(full); return NULL; }
    wchar_t *out = calloc(len + prefix + 1, sizeof(*out));
    if (out) {
        wcscpy(out, unc ? L"\\\\?\\UNC\\" : L"\\\\?\\");
        wcscat(out, full + (unc ? 2 : 0));
    }
    free(full); return out;
}
static FILE *open_file(const char *path, const char *mode) {
    wchar_t *p = wide_path(path), *m = wide(mode);
    FILE *f = p && m ? _wfopen(p, m) : NULL;
    free(p); free(m); return f;
}
static FILE *new_file(const char *path) {
    wchar_t *p = wide_path(path);
    HANDLE h = p ? CreateFileW(p, GENERIC_WRITE, 0, NULL, CREATE_NEW, FILE_ATTRIBUTE_NORMAL, NULL) : INVALID_HANDLE_VALUE;
    free(p);
    if (h == INVALID_HANDLE_VALUE) return NULL;
    int fd = _open_osfhandle((intptr_t)h, 0);
    if (fd < 0) { CloseHandle(h); return NULL; }
    FILE *f = _fdopen(fd, "wb");
    if (!f) _close(fd);
    return f;
}
static void remove_file(const char *p) {
    wchar_t *w = wide_path(p); if (w) DeleteFileW(w); free(w);
}
static int replace_file(const char *a, const char *b) {
    wchar_t *wa = wide_path(a), *wb = wide_path(b);
    int ok = wa && wb && MoveFileExW(wa, wb, MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH);
    free(wa); free(wb); return ok ? 0 : -1;
}
static int mkdir_one(const char *p) {
    wchar_t *w = wide_path(p);
    int ok = w && (CreateDirectoryW(w, NULL) || GetLastError() == ERROR_ALREADY_EXISTS);
    free(w); return ok ? 0 : -1;
}
static unsigned long process_id(void) { return GetCurrentProcessId(); }
#else
static FILE *open_file(const char *p, const char *m) { return fopen(p, m); }
static FILE *new_file(const char *p) {
    int fd = open(p, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    if (fd < 0) return NULL;
    FILE *f = fdopen(fd, "wb");
    if (!f) close(fd);
    return f;
}
static void remove_file(const char *p) { unlink(p); }
static int replace_file(const char *a, const char *b) { return rename(a, b); }
static int mkdir_one(const char *p) { return mkdir(p, 0700) == 0 || errno == EEXIST ? 0 : -1; }
static unsigned long process_id(void) { return (unsigned long)getpid(); }
#endif

static int path_join(char *out, const char *base, const char *name) {
    int n = snprintf(out, PATH_CAP, "%s/%s", base, name);
    return n > 0 && n < PATH_CAP ? 0 : -1;
}
static int mkdir_tree(char *path) {
    for (char *p = path + 1; *p; ++p) {
        if (*p != '/' && *p != '\\') continue;
        char c = *p; *p = 0;
        int rc = strlen(path) == 2 && path[1] == ':' ? 0 : mkdir_one(path);
        *p = c;
        if (rc) return -1;
    }
    return mkdir_one(path);
}
static int cache_dir(char *out) {
#ifdef _WIN32
    wchar_t base[PATH_CAP];
    DWORD n = GetEnvironmentVariableW(L"LOCALAPPDATA", base, PATH_CAP);
    char utf[PATH_CAP];
    if (!n || n >= PATH_CAP || !WideCharToMultiByte(CP_UTF8, 0, base, -1, utf, PATH_CAP, NULL, NULL)) return -1;
    if (path_join(out, utf, "simforever-companion/releases")) return -1;
#else
    const char *base = getenv("XDG_CACHE_HOME");
    if (base && base[0] == '/') {
        if (path_join(out, base, "simforever-companion/releases")) return -1;
    } else {
        base = getenv("HOME");
        if (!base || path_join(out, base, ".cache/simforever-companion/releases")) return -1;
    }
#endif
    return mkdir_tree(out);
}

static int self_path(char *out) {
#ifdef _WIN32
    wchar_t w[PATH_CAP]; DWORD n = GetModuleFileNameW(NULL, w, PATH_CAP);
    return n && n < PATH_CAP && WideCharToMultiByte(CP_UTF8, 0, w, -1, out, PATH_CAP, NULL, NULL) ? 0 : -1;
#elif defined(__APPLE__)
    uint32_t n = PATH_CAP; return _NSGetExecutablePath(out, &n) == 0 ? 0 : -1;
#else
    ssize_t n = readlink("/proc/self/exe", out, PATH_CAP - 1);
    if (n < 0 || n >= PATH_CAP - 1) return -1;
    out[n] = 0; return 0;
#endif
}

static int sha256(FILE *f, uint64_t count, unsigned char out[32]) {
    unsigned char buf[65536]; int ok = 1;
#ifdef _WIN32
    BCRYPT_ALG_HANDLE alg = NULL; BCRYPT_HASH_HANDLE ctx = NULL;
    if (BCryptOpenAlgorithmProvider(&alg, BCRYPT_SHA256_ALGORITHM, NULL, 0) < 0) return -1;
    if (BCryptCreateHash(alg, &ctx, NULL, 0, NULL, 0, 0) < 0) { BCryptCloseAlgorithmProvider(alg, 0); return -1; }
#elif defined(__APPLE__)
    CC_SHA256_CTX ctx; CC_SHA256_Init(&ctx);
#else
    EVP_MD_CTX *ctx = EVP_MD_CTX_new();
    if (!ctx) return -1;
    if (EVP_DigestInit_ex(ctx, EVP_sha256(), NULL) != 1) { EVP_MD_CTX_free(ctx); return -1; }
#endif
    while (count) {
        size_t want = count < sizeof(buf) ? (size_t)count : sizeof(buf);
        size_t n = fread(buf, 1, want, f);
        if (n != want) { ok = 0; break; }
#ifdef _WIN32
        if (BCryptHashData(ctx, buf, (ULONG)n, 0) < 0) { ok = 0; break; }
#elif defined(__APPLE__)
        CC_SHA256_Update(&ctx, buf, (CC_LONG)n);
#else
        if (EVP_DigestUpdate(ctx, buf, n) != 1) { ok = 0; break; }
#endif
        count -= n;
    }
#ifdef _WIN32
    if (ok && BCryptFinishHash(ctx, out, 32, 0) < 0) ok = 0;
    BCryptDestroyHash(ctx); BCryptCloseAlgorithmProvider(alg, 0);
#elif defined(__APPLE__)
    if (ok) CC_SHA256_Final(out, &ctx);
#else
    if (ok && EVP_DigestFinal_ex(ctx, out, NULL) != 1) ok = 0;
    EVP_MD_CTX_free(ctx);
#endif
    return ok ? 0 : -1;
}
static void hex_digest(const unsigned char d[32], char out[65]) {
    static const char hex[] = "0123456789abcdef";
    for (int i = 0; i < 32; ++i) { out[i * 2] = hex[d[i] >> 4]; out[i * 2 + 1] = hex[d[i] & 15]; }
    out[64] = 0;
}
static int file_size(FILE *f, uint64_t *size) {
    if (fseek(f, 0, SEEK_END)) return -1;
    long n = ftell(f); if (n < 0) return -1;
    *size = (uint64_t)n; rewind(f); return 0;
}
static int verify(const char *path, const Release *r) {
    FILE *f = open_file(path, "rb");
    if (!f) return -1;
    uint64_t size; unsigned char d[32]; char hex[65];
    int rc = file_size(f, &size) || size != r->size || sha256(f, size, d);
    fclose(f); if (rc) return -1;
    hex_digest(d, hex); return strcmp(hex, r->hash) == 0 ? 0 : -1;
}
static int version_parts(const char *s, unsigned parts[3]) {
    for (int i = 0; i < 3; ++i) {
        if (*s < '0' || *s > '9') return -1;
        if (*s == '0' && s[1] >= '0' && s[1] <= '9') return -1;
        unsigned v = 0;
        do { v = v * 10 + (unsigned)(*s++ - '0'); if (v > 999999) return -1; } while (*s >= '0' && *s <= '9');
        parts[i] = v;
        if (i < 2) { if (*s++ != '.') return -1; }
    }
    return *s ? -1 : 0;
}
static int compare_version(const char *a, const char *b) {
    unsigned x[3], y[3];
    if (version_parts(a, x) || version_parts(b, y)) return INT_MIN;
    for (int i = 0; i < 3; ++i) if (x[i] != y[i]) return x[i] > y[i] ? 1 : -1;
    return 0;
}
static int read_release(FILE *f, Release *r) {
    char text[256]; size_t n = fread(text, 1, sizeof(text) - 1, f);
    if (ferror(f) || !feof(f) || memchr(text, 0, n)) return -1;
    text[n] = 0;
    char *line[4], *p = text;
    for (int i = 0; i < 4; ++i) {
        line[i] = p; p = strchr(p, '\n'); if (!p) return -1; *p++ = 0;
    }
    unsigned parts[3];
    if (*p || strcmp(line[0], "SIMFOREVER-COMPANION-1") || version_parts(line[1], parts) || strlen(line[1]) >= sizeof(r->version)) return -1;
    uint64_t size = 0;
    if (!*line[2]) return -1;
    for (p = line[2]; *p; ++p) {
        if (*p < '0' || *p > '9') return -1;
        size = size * 10 + (unsigned)(*p - '0'); if (size > MAX_BINARY) return -1;
    }
    if (!size || strlen(line[3]) != 64) return -1;
    for (p = line[3]; *p; ++p) if (!((*p >= '0' && *p <= '9') || (*p >= 'a' && *p <= 'f'))) return -1;
    strcpy(r->version, line[1]); strcpy(r->hash, line[3]); r->size = size; return 0;
}
static int write_release(const char *path, const Release *r) {
    FILE *f = new_file(path); if (!f) return -1;
    int rc = fprintf(f, "SIMFOREVER-COMPANION-1\n%s\n%llu\n%s\n", r->version, (unsigned long long)r->size, r->hash) < 0;
    if (fclose(f)) rc = 1;
    return rc ? -1 : 0;
}

#ifndef _WIN32
static size_t receive(void *data, size_t width, size_t count, void *arg) {
    Sink *s = arg;
    if (width && count > SIZE_MAX / width) return 0;
    size_t n = width * count;
    if (n > s->limit - s->total || fwrite(data, 1, n, s->file) != n) return 0;
    s->total += n; return n;
}
#endif
static int download(const char *url, const char *path, size_t limit) {
    FILE *f = new_file(path); if (!f) return -1;
    int ok = 0;
#ifdef _WIN32
    wchar_t *w = wide(url); URL_COMPONENTS u = {0}; u.dwStructSize = sizeof(u);
    u.dwHostNameLength = u.dwUrlPathLength = (DWORD)-1;
    HINTERNET session = NULL, conn = NULL, req = NULL;
    if (!w || !WinHttpCrackUrl(w, 0, 0, &u) || u.nScheme != INTERNET_SCHEME_HTTPS || u.dwHostNameLength >= 256) goto done;
    wchar_t host[256]; memcpy(host, u.lpszHostName, u.dwHostNameLength * sizeof(wchar_t)); host[u.dwHostNameLength] = 0;
    session = WinHttpOpen(L"Simforever-Updater/1", WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY, NULL, NULL, 0);
    if (!session || !WinHttpSetTimeouts(session, 5000, 5000, 10000, 15000)) goto done;
    conn = WinHttpConnect(session, host, u.nPort, 0); if (!conn) goto done;
    req = WinHttpOpenRequest(conn, L"GET", u.lpszUrlPath, NULL, NULL, NULL, WINHTTP_FLAG_SECURE);
    DWORD policy = WINHTTP_OPTION_REDIRECT_POLICY_NEVER;
    if (!req || !WinHttpSetOption(req, WINHTTP_OPTION_REDIRECT_POLICY, &policy, sizeof(policy))) goto done;
    if (!WinHttpSendRequest(req, NULL, 0, NULL, 0, 0, 0) || !WinHttpReceiveResponse(req, NULL)) goto done;
    DWORD status = 0, len = sizeof(status);
    if (!WinHttpQueryHeaders(req, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER, NULL, &status, &len, NULL) || status != 200) goto done;
    size_t total = 0; unsigned char buf[65536]; DWORD n;
    ULONGLONG deadline = GetTickCount64() + (limit < 4096 ? 15000 : 120000);
    for (;;) {
        if (GetTickCount64() > deadline || !WinHttpReadData(req, buf, sizeof(buf), &n)) goto done;
        if (!n) break;
        if (n > limit - total || fwrite(buf, 1, n, f) != n) goto done;
        total += n;
    }
    ok = 1;
done:
    if (req) WinHttpCloseHandle(req);
    if (conn) WinHttpCloseHandle(conn);
    if (session) WinHttpCloseHandle(session);
    free(w);
#else
    CURL *c = curl_easy_init();
    if (c) {
        Sink sink = {f, 0, limit};
        curl_easy_setopt(c, CURLOPT_URL, url);
        curl_easy_setopt(c, CURLOPT_PROTOCOLS_STR, "https");
        curl_easy_setopt(c, CURLOPT_FOLLOWLOCATION, 0L);
        curl_easy_setopt(c, CURLOPT_FAILONERROR, 1L);
        curl_easy_setopt(c, CURLOPT_CONNECTTIMEOUT, 5L);
        curl_easy_setopt(c, CURLOPT_TIMEOUT, limit < 4096 ? 15L : 120L);
        curl_easy_setopt(c, CURLOPT_LOW_SPEED_LIMIT, 1024L);
        curl_easy_setopt(c, CURLOPT_LOW_SPEED_TIME, 15L);
        curl_easy_setopt(c, CURLOPT_USERAGENT, "Simforever-Updater/1");
        curl_easy_setopt(c, CURLOPT_WRITEFUNCTION, receive);
        curl_easy_setopt(c, CURLOPT_WRITEDATA, &sink);
        long status = 0;
        if (curl_easy_perform(c) == CURLE_OK && curl_easy_getinfo(c, CURLINFO_RESPONSE_CODE, &status) == CURLE_OK && status == 200) ok = 1;
        curl_easy_cleanup(c);
    }
#endif
    if (fclose(f)) ok = 0;
    if (!ok) remove_file(path);
    return ok ? 0 : -1;
}

static int copy_bytes(FILE *in, FILE *out, uint64_t size) {
    unsigned char buf[65536];
    while (size) {
        size_t n = size < sizeof(buf) ? (size_t)size : sizeof(buf);
        if (fread(buf, 1, n, in) != n || fwrite(buf, 1, n, out) != n) return -1;
        size -= n;
    }
    return 0;
}
static int package(const char *launcher, const char *worker, const char *out) {
    FILE *l = open_file(launcher, "rb"), *w = open_file(worker, "rb"), *f = NULL;
    uint64_t ln = 0, wn = 0; unsigned char d[32], footer[FOOTER_SIZE] = {0}; int rc = -1;
    if (!l || !w || file_size(l, &ln) || file_size(w, &wn) || !wn || ln + wn + FOOTER_SIZE > MAX_BINARY || sha256(w, wn, d)) goto done;
    rewind(w); f = open_file(out, "wb"); if (!f) goto done;
    memcpy(footer, "SFCP1\n", 6);
    for (int i = 0; i < 8; ++i) footer[6 + i] = (unsigned char)(wn >> (8 * i));
    memcpy(footer + 14, d, 32);
    if (copy_bytes(l, f, ln) || copy_bytes(w, f, wn) || fwrite(footer, 1, sizeof(footer), f) != sizeof(footer)) goto done;
    rc = 0;
done:
    if (l) fclose(l);
    if (w) fclose(w);
    if (f && fclose(f)) rc = -1;
    if (rc) remove_file(out);
#ifndef _WIN32
    else if (chmod(out, 0700)) rc = -1;
#endif
    return rc;
}

static int extract(const char *self, const char *cache, char *out) {
    FILE *f = open_file(self, "rb"); if (!f) return -1;
    uint64_t size = 0, worker_size = 0; unsigned char footer[FOOTER_SIZE]; Release r = {0};
    int rc = -1; char name[128], tmp[PATH_CAP] = {0}; FILE *w = NULL;
    if (file_size(f, &size) || size < FOOTER_SIZE || size > MAX_BINARY) goto done;
#ifdef __APPLE__
    const struct section_64 *section = getsectbynamefromheader_64(&_mh_execute_header, "__DATA", "__sfworker");
    if (section) {
        uint64_t end = (uint64_t)section->offset + section->size;
        if (section->size < FOOTER_SIZE || end > size) goto done;
        size = end;
    }
#endif
    if (fseek(f, (long)(size - FOOTER_SIZE), SEEK_SET) || fread(footer, 1, sizeof(footer), f) != sizeof(footer) || memcmp(footer, "SFCP1\n", 6)) goto done;
    for (int i = 0; i < 8; ++i) worker_size |= (uint64_t)footer[6 + i] << (8 * i);
    if (!worker_size || worker_size > size - FOOTER_SIZE) goto done;
    r.size = worker_size; hex_digest(footer + 14, r.hash);
    snprintf(name, sizeof(name), "%s-%s-worker%s", SF_VERSION, SF_TARGET, EXT);
    if (path_join(out, cache, name)) goto done;
    if (!verify(out, &r)) { rc = 0; goto done; }
    if (snprintf(tmp, sizeof(tmp), "%s.part.%lu", out, process_id()) >= (int)sizeof(tmp)) goto done;
    if (fseek(f, (long)(size - FOOTER_SIZE - worker_size), SEEK_SET)) goto done;
    w = new_file(tmp); if (!w) goto done;
    if (copy_bytes(f, w, worker_size)) goto done;
    if (fclose(w)) { w = NULL; goto done; } w = NULL;
    if (verify(tmp, &r)) goto done;
#ifndef _WIN32
    if (chmod(tmp, 0700)) goto done;
#endif
    if (replace_file(tmp, out)) goto done;
    rc = 0;
done:
    if (w) fclose(w);
    if (rc && tmp[0]) remove_file(tmp);
    fclose(f); return rc;
}

static int release_path(char *out, const char *cache, const Release *r) {
    char name[128]; snprintf(name, sizeof(name), "%s-%s%s", r->version, SF_TARGET, EXT);
    return path_join(out, cache, name);
}
static int pointer_path(char *out, const char *cache) {
    return path_join(out, cache, SF_TARGET "-current.txt");
}
static int cached_release(const char *cache, Release *r, char *out) {
    char path[PATH_CAP]; if (pointer_path(path, cache)) return -1;
    FILE *f = open_file(path, "rb"); if (!f) return -1;
    int rc = read_release(f, r); fclose(f);
    if (rc || compare_version(r->version, SF_VERSION) <= 0 || release_path(out, cache, r) || verify(out, r)) return -1;
    return 0;
}
static int update(const char *cache, char *out, int (*fetch)(const char *, const char *, size_t)) {
    char url[512], tmp[PATH_CAP], latest[PATH_CAP], pointer[PATH_CAP], saved[PATH_CAP]; Release r, cached;
    int have_cached = cached_release(cache, &cached, saved) == 0;
    snprintf(url, sizeof(url), "%s/downloads/companion/%s/latest.txt", SF_ORIGIN, SF_TARGET);
    char name[128]; snprintf(name, sizeof(name), "latest.part.%lu", process_id());
    if (path_join(tmp, cache, name) || pointer_path(latest, cache)) return -1;
    if (fetch(url, tmp, 255)) goto fallback;
    FILE *f = open_file(tmp, "rb"); if (!f) goto fallback;
    int rc = read_release(f, &r); fclose(f); remove_file(tmp);
    if (rc) goto fallback;
    if (have_cached && compare_version(r.version, cached.version) < 0) goto fallback;
    if (compare_version(r.version, SF_VERSION) <= 0) return 0;
    if (release_path(out, cache, &r)) goto fallback;
    if (verify(out, &r)) {
        snprintf(url, sizeof(url), "%s/downloads/companion/%s/simforever-companion-%s%s", SF_ORIGIN, r.version, SF_TARGET, EXT);
        if (snprintf(tmp, sizeof(tmp), "%s.part.%lu", out, process_id()) >= (int)sizeof(tmp)) goto fallback;
        if (fetch(url, tmp, (size_t)r.size) || verify(tmp, &r)) { remove_file(tmp); goto fallback; }
#ifndef _WIN32
        if (chmod(tmp, 0700)) { remove_file(tmp); goto fallback; }
#endif
        if (replace_file(tmp, out)) { remove_file(tmp); goto fallback; }
    }
    snprintf(name, sizeof(name), "current.part.%lu", process_id());
    if (path_join(pointer, cache, name) || write_release(pointer, &r) || replace_file(pointer, latest)) {
        remove_file(pointer); goto fallback;
    }
    fprintf(stderr, "Companion update ready: %s\n", r.version);
    return 1;
fallback:
    remove_file(tmp);
    fprintf(stderr, "Could not check or verify an update; keeping the installed companion.\n");
    if (have_cached) { strcpy(out, saved); return 1; }
    return -1;
}

#include "capture.h"
#include "addon.h"
#ifndef _WIN32
#include "logs_posix.h"
#endif
static int no_collect, collect_yes, no_addon, no_logs;
#ifdef _WIN32
static int background_start, no_desktop, console_only;
static HANDLE desktop_stop, desktop_job;
static const char *desktop_worker_config;
#endif

#ifdef _WIN32
static int launch(const char *path, int argc, char **argv, int handoff) {
    wchar_t *exe = wide(path), command[PATH_CAP]; size_t used = 0;
    if (!exe) return 1;
    for (int i = 0; i < argc + handoff; ++i) {
        const char *value = i == argc ? "--from-update" : argv[i];
        if (!handoff && desktop_worker_config && i > 0 && i < argc && !strcmp(argv[i-1], "-config")) value = desktop_worker_config;
        wchar_t *arg = i == 0 ? wide(path) : wide(value);
        if (!arg) { free(exe); return 1; }
        if (used + 3 >= PATH_CAP) { free(arg); free(exe); return 1; }
        if (i) command[used++] = ' ';
        command[used++] = '"'; size_t slashes = 0;
        for (wchar_t *p = arg;; ++p) {
            if (*p == '\\') { ++slashes; continue; }
            size_t n = *p == '"' || !*p ? slashes * 2 : slashes;
            if (*p == '"') ++n;
            if (used + n + 3 >= PATH_CAP) { free(arg); free(exe); return 1; }
            while (n--) command[used++] = '\\';
            slashes = 0;
            if (!*p) break;
            command[used++] = *p;
        }
        command[used++] = '"'; free(arg);
    }
    command[used] = 0;
    STARTUPINFOW si = {0}; PROCESS_INFORMATION pi = {0}; si.cb = sizeof(si);
    si.dwFlags = STARTF_USESTDHANDLES;
    si.hStdInput = GetStdHandle(STD_INPUT_HANDLE);
    si.hStdOutput = GetStdHandle(STD_OUTPUT_HANDLE);
    si.hStdError = GetStdHandle(STD_ERROR_HANDLE);
    DWORD flags = desktop_stop && !handoff ? CREATE_NO_WINDOW | CREATE_SUSPENDED : 0;
    int ok = CreateProcessW(exe, command, NULL, NULL, TRUE, flags, NULL, NULL, &si, &pi);
    free(exe); if (!ok) return -1;
    if (flags) {
        if (!AssignProcessToJobObject(desktop_job, pi.hProcess)) {
            TerminateProcess(pi.hProcess, 1); CloseHandle(pi.hThread); CloseHandle(pi.hProcess); return -1;
        }
        ResumeThread(pi.hThread);
    }
    CloseHandle(pi.hThread);
    AddonManager addon = {0};
    if (!handoff && !desktop_stop && !no_addon && addon_init(&addon, argc, argv)) addon_start(&addon);
    CaptureCollector collector;
    int collect = !handoff && !no_collect && capture_init(&collector, argc, argv);
    if (collect) { collector.grant = collect_yes; collector.quiet = desktop_stop != NULL; }
    if (collect) capture_tick(&collector, capture_post);
    while (WaitForSingleObject(pi.hProcess, 1000) == WAIT_TIMEOUT) {
        if (desktop_stop && WaitForSingleObject(desktop_stop, 0) == WAIT_OBJECT_0) {
            TerminateProcess(pi.hProcess, 0); WaitForSingleObject(pi.hProcess, INFINITE); break;
        }
        if (collect) capture_tick(&collector, capture_post);
        addon_poll(&addon);
    }
    if (collect) {
        if (!desktop_stop || WaitForSingleObject(desktop_stop, 0) != WAIT_OBJECT_0) { collector.poll = 0; capture_tick(&collector, capture_post); }
        capture_close(&collector);
    }
    DWORD code = 1; GetExitCodeProcess(pi.hProcess, &code); CloseHandle(pi.hProcess); return (int)code;
}
#else
static volatile sig_atomic_t capture_signal;
static void capture_stop_signal(int sig) { capture_signal = sig; }
static int launch(const char *path, int argc, char **argv, int handoff) {
    char **args = calloc((size_t)argc + (size_t)handoff + 2, sizeof(*args)); if (!args) return 1;
    args[0] = (char *)path;
    for (int i = 1; i < argc; ++i) args[i] = argv[i];
    int next = argc;
    if (handoff) args[next++] = "--from-update";
    AddonManager addon = {0};
    if (!handoff && !no_addon) (void)addon_init(&addon, argc, argv);
    CaptureCollector collector;
    int collect = !handoff && !no_collect && capture_init(&collector, argc, argv);
    /* C sends combat logs, so the legacy worker is told not to. */
    LogsSender logs; int send_logs = !handoff && !no_logs && logs_init(&logs, argc, argv);
    if (send_logs) args[next++] = "-logs=false";
    if (!collect && !addon.active && !send_logs) {
        execv(path, args); perror("Cannot start companion"); free(args); return -1;
    }
    if (collect) collector.grant = collect_yes;
    pid_t child = fork();
    if (child < 0) { free(args); return -1; }
    if (!child) { execv(path, args); perror("Cannot start companion"); _exit(127); }
    free(args);
    addon_start(&addon);
    void (*old_int)(int) = signal(SIGINT, capture_stop_signal);
    void (*old_term)(int) = signal(SIGTERM, capture_stop_signal);
    int status = 0;
    for (;;) {
        pid_t done = waitpid(child, &status, WNOHANG);
        if (done == child) break;
        if (done < 0 && errno != EINTR) { status = 1 << 8; break; }
        if (capture_signal) { kill(child, capture_signal); capture_signal = 0; }
        if (collect) capture_tick(&collector, capture_post);
        addon_poll(&addon);
        if (send_logs) logs_tick(&logs);
        sleep(1);
    }
    if (send_logs) logs_free(&logs);
    if (collect) { collector.poll = 0; capture_tick(&collector, capture_post); capture_close(&collector); }
    signal(SIGINT, old_int); signal(SIGTERM, old_term);
    return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
}
#endif

#ifdef _WIN32
#include "desktop_windows.h"
#endif

static int launch_update(const char *next, int argc, char **argv) {
    int count = argc;
    if (no_collect) argv[count++] = "--no-collect";
    if (collect_yes) argv[count++] = "--collect-yes";
    if (no_addon) argv[count++] = "--no-addon";
    if (no_logs) argv[count++] = "--no-logs";
#ifdef _WIN32
    if (background_start) argv[count++] = "--background";
    if (no_desktop) argv[count++] = "--no-desktop";
#endif
    argv[count] = NULL;
    int code = launch(next, count, argv, 1);
    argv[argc] = NULL; return code;
}
static int run(int argc, char **argv) {
    if (argc == 5 && !strcmp(argv[1], "--package")) return package(argv[2], argv[3], argv[4]) ? 1 : 0;
    if (argc == 4 && !strcmp(argv[1], "--capture-probe")) return capture_export(argv[2], argv[3]);
    if (argc == 3 && !strcmp(argv[1], "--install-addon")) {
        int rc = addon_install(argv[2]);
        if (rc < 0) fprintf(stderr, "Could not install the addon. Choose the _forever_ folder and check its write permissions.\n");
        return rc < 0 ? 1 : 0;
    }
    if (argc == 2 && !strcmp(argv[1], "--addon-version")) { puts(sf_probe_version); return 0; }
    int version = argc == 2 && (!strcmp(argv[1], "-version") || !strcmp(argv[1], "--version"));
    int skip = 0, check = 0, count = 1;
    for (int i = 1; i < argc; ++i) {
        if (!strcmp(argv[i], "--no-update") || !strcmp(argv[i], "--from-update")) skip = 1;
        else if (!strcmp(argv[i], "--no-collect")) no_collect = 1;
        else if (!strcmp(argv[i], "--collect-yes")) collect_yes = 1;
        else if (!strcmp(argv[i], "--no-addon")) no_addon = 1;
        else if (!strcmp(argv[i], "--no-logs")) no_logs = 1;
        else if (!strcmp(argv[i], "--check-update")) check = 1;
#ifdef _WIN32
        else if (!strcmp(argv[i], "--background")) background_start = 1;
        else if (!strcmp(argv[i], "--no-desktop")) no_desktop = 1;
        else if (!strcmp(argv[i], "-console") || !strcmp(argv[i], "--console")) { console_only = 1; argv[count++] = argv[i]; }
#endif
        else {
            char *arg = argv[i];
            argv[count++] = arg;
            if (!strcmp(arg, "--")) {
                while (++i < argc) argv[count++] = argv[i];
                break;
            }
            const char *name = arg;
            if (*name == '-') ++name;
            if (*name == '-') ++name;
            size_t len = strcspn(name, "=");
            const char *diagnostics[] = {"show", "once", "dry-run", "print-payload", "version", "h", "help"};
            const char *values[] = {"wow", "config", "api", "game", "name", "interval", "print-payload"};
            if (arg[0] == '-') {
                for (size_t j = 0; j < sizeof(diagnostics) / sizeof(*diagnostics); ++j)
                    if (strlen(diagnostics[j]) == len && !strncmp(name, diagnostics[j], len)) skip = 1;
                if (!name[len]) {
                    for (size_t j = 0; j < sizeof(values) / sizeof(*values); ++j)
                        if (strlen(values[j]) == len && !strncmp(name, values[j], len) && i + 1 < argc) { argv[count++] = argv[++i]; break; }
                }
            }
        }
    }
    argv[count] = NULL; char self[PATH_CAP], cache[PATH_CAP], next[PATH_CAP];
    if (self_path(self) || cache_dir(cache)) { fprintf(stderr, "Cannot locate the companion cache.\n"); return 1; }
    Release cached;
    if (version) {
        printf("simforever-companion %s\n", cached_release(cache, &cached, next) == 0 ? cached.version : SF_VERSION);
        return 0;
    }
    if (!skip || check) {
        int rc = update(cache, next, download);
        if (check) { puts(rc > 0 ? "A newer companion is ready for the next launch." : rc == 0 ? "Companion is current." : "Update check unavailable."); return rc < 0 ? 1 : 0; }
        if (rc > 0) {
            int code = launch_update(next, count, argv);
            if (code >= 0) return code;
            fprintf(stderr, "The update could not start; using the bundled companion.\n");
        }
    } else if (cached_release(cache, &cached, next) == 0) {
        int code = launch_update(next, count, argv);
        if (code >= 0) return code;
    }
    if (extract(self, cache, next)) { fprintf(stderr, "Cannot verify the bundled companion.\n"); return 1; }
#ifdef _WIN32
    AddonManager *desktop_args = calloc(1, sizeof(*desktop_args));
    int desktop_eligible = desktop_args && addon_init(desktop_args, count, argv);
    free(desktop_args);
    if (console_only) desktop_eligible = 0;
    if (!no_desktop && desktop_eligible) return desktop_run(next, count, argv, background_start);
#endif
    char **worker_args = argv, *game = NULL;
    int worker_count = no_addon ? count : addon_release_args(count, argv, &worker_args, &game);
    int result = launch(next, worker_count, worker_args, 0);
    if (game) { free(worker_args); free(game); }
    return result;
}
#ifdef _WIN32
int wmain(int argc, wchar_t **wargv) {
    char **argv = calloc((size_t)argc + 8, sizeof(*argv)); if (!argv) return 1;
    for (int i = 0; i < argc; ++i) {
        int n = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wargv[i], -1, NULL, 0, NULL, NULL);
        argv[i] = n ? malloc((size_t)n) : NULL;
        if (!argv[i] || !WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wargv[i], -1, argv[i], n, NULL, NULL)) return 1;
    }
    DWORD output_type = GetFileType(GetStdHandle(STD_OUTPUT_HANDLE));
    if (argc > 1 && output_type != FILE_TYPE_PIPE && output_type != FILE_TYPE_DISK && AttachConsole(ATTACH_PARENT_PROCESS)) {
        freopen("CONOUT$", "w", stdout); freopen("CONOUT$", "w", stderr); freopen("CONIN$", "r", stdin);
    }
    return run(argc, argv);
}
#else
int main(int argc, char **argv) {
    char **args = calloc((size_t)argc + 8, sizeof(*args)); if (!args) return 1;
    for (int i = 0; i < argc; ++i) args[i] = argv[i];
    int code = run(argc, args); free(args); return code;
}
#endif
