/* SavedVariables evidence collector. Included after the launcher's platform
 * helpers so native paths, hashing and atomic replacement share one boundary.
 * cJSON is vendored under the MIT license; Lua is parsed, never executed. */
#ifndef SF_CAPTURE_H
#define SF_CAPTURE_H
#define CJSON_NESTING_LIMIT 64
#include "vendor/cJSON.c"
#include <ctype.h>
#include <time.h>
#include <signal.h>
#ifndef _WIN32
#include <dirent.h>
#include <sys/wait.h>
#endif
#define CAPTURE_LIMIT (16u * 1024u * 1024u)
#define CAPTURE_SOURCE_LIMIT (64u * 1024u * 1024u)
#define CAPTURE_QUOTA (256u * 1024u * 1024u)

typedef struct { char *s; size_t n, cap; int failed; } CaptureBuffer;
static int cb_add(CaptureBuffer *b, const void *data, size_t n) {
    if (b->failed || n > CAPTURE_LIMIT || b->n > CAPTURE_LIMIT - n) { b->failed = 1; return -1; }
    if (b->n + n + 1 > b->cap) {
        size_t cap = b->cap ? b->cap : 128;
        while (cap < b->n + n + 1) cap *= 2;
        char *p = realloc(b->s, cap); if (!p) { b->failed = 1; return -1; }
        b->s = p; b->cap = cap;
    }
    memcpy(b->s + b->n, data, n); b->n += n; b->s[b->n] = 0; return 0;
}
static char *capture_read_limit(const char *path, size_t *length, size_t limit) {
    FILE *f = open_file(path, "rb"); if (!f) return NULL;
    uint64_t size = 0; char *out = NULL;
    if (!file_size(f, &size) && size <= limit) {
        out = malloc((size_t)size + 1);
        if (out && (fread(out, 1, (size_t)size, f) != size || fgetc(f) != EOF || ferror(f))) { free(out); out = NULL; }
        if (out) { out[size] = 0; *length = (size_t)size; }
    }
    fclose(f); return out;
}
static char *capture_read(const char *path, size_t *length) { return capture_read_limit(path, length, CAPTURE_LIMIT); }
static int capture_atomic(const char *path, const char *data, size_t n) {
    char tmp[PATH_CAP];
    if (snprintf(tmp, sizeof(tmp), "%s.part.%lu", path, process_id()) >= (int)sizeof(tmp)) return -1;
    FILE *f = new_file(tmp); if (!f) return -1;
    int rc = fwrite(data, 1, n, f) != n || fflush(f);
#ifdef _WIN32
    if (!rc && _commit(_fileno(f))) rc = 1;
#else
    if (!rc && fsync(fileno(f))) rc = 1;
#endif
    if (fclose(f)) rc = 1;
    if (!rc && replace_file(tmp, path)) rc = 1;
    if (rc) remove_file(tmp);
    return rc ? -1 : 0;
}
static int capture_hash_bytes(const char *data, size_t n, char hex[65]) {
    unsigned char digest[32];
#ifdef _WIN32
    BCRYPT_ALG_HANDLE alg = NULL; BCRYPT_HASH_HANDLE ctx = NULL; int rc = -1;
    if (n > ULONG_MAX || BCryptOpenAlgorithmProvider(&alg, BCRYPT_SHA256_ALGORITHM, NULL, 0) < 0) return -1;
    if (BCryptCreateHash(alg, &ctx, NULL, 0, NULL, 0, 0) >= 0 &&
        BCryptHashData(ctx, (PUCHAR)data, (ULONG)n, 0) >= 0 && BCryptFinishHash(ctx, digest, 32, 0) >= 0) rc = 0;
    if (ctx) BCryptDestroyHash(ctx);
    BCryptCloseAlgorithmProvider(alg, 0); if (rc) return -1;
#elif defined(__APPLE__)
    if (n > UINT32_MAX || !CC_SHA256(data, (CC_LONG)n, digest)) return -1;
#else
    if (EVP_Digest(data, n, digest, NULL, EVP_sha256(), NULL) != 1) return -1;
#endif
    hex_digest(digest, hex); return 0;
}

typedef struct { const char *p, *end; unsigned depth, nodes; int failed; } LuaCapture;
static void lc_space(LuaCapture *l) {
    for (;;) {
        while (l->p < l->end && isspace((unsigned char)*l->p)) ++l->p;
        if (l->end - l->p < 2 || l->p[0] != '-' || l->p[1] != '-') return;
        l->p += 2;
        if (l->end - l->p >= 2 && l->p[0] == '[' && l->p[1] == '[') {
            l->p += 2;
            while (l->end - l->p >= 2 && !(l->p[0] == ']' && l->p[1] == ']')) ++l->p;
            if (l->end - l->p < 2) { l->failed = 1; return; }
            l->p += 2;
        } else while (l->p < l->end && *l->p != '\n') ++l->p;
    }
}
static char *lc_string(LuaCapture *l) {
    CaptureBuffer b = {0}; char quote = *l->p++; int closed = 0;
    while (l->p < l->end) {
        unsigned char c = (unsigned char)*l->p++;
        if (c == (unsigned char)quote) { closed = 1; break; }
        if (c == '\\') {
            if (l->p == l->end) break;
            c = (unsigned char)*l->p++;
            if (c >= '0' && c <= '9') {
                unsigned v = c - '0';
                for (int i = 1; i < 3 && l->p < l->end && *l->p >= '0' && *l->p <= '9'; ++i) v = v * 10 + (unsigned)(*l->p++ - '0');
                if (v > 255) { l->failed = 1; break; }
                c = (unsigned char)v;
            } else {
                switch (c) {
                    case 'n': c = '\n'; break; case 'r': c = '\r'; break; case 't': c = '\t'; break;
                    case 'a': c = '\a'; break; case 'b': c = '\b'; break; case 'f': c = '\f'; break; case 'v': c = '\v'; break;
                    case '\\': case '"': case '\'': case '\n': break;
                    default: l->failed = 1; break;
                }
            }
        }
        if (!c || l->failed || cb_add(&b, &c, 1)) { l->failed = 1; break; }
    }
    if (!closed || l->failed || b.failed) { free(b.s); l->failed = 1; return NULL; }
    if (!b.s && cb_add(&b, "", 0)) { l->failed = 1; return NULL; }
    return b.s;
}
static cJSON *lc_value(LuaCapture *l);
static cJSON *lc_table(LuaCapture *l) {
    if (++l->depth > 64) { l->failed = 1; return NULL; }
    ++l->p; cJSON *out = cJSON_CreateObject(); int array = 1; size_t count = 0, next = 1, highest = 0;
    if (!out) { l->failed = 1; return NULL; }
    for (;;) {
        lc_space(l); if (l->p >= l->end || l->failed) break;
        if (*l->p == '}') {
            ++l->p; --l->depth;
            if (array && count && highest == count) {
                cJSON *list = cJSON_CreateArray(); if (!list) break;
                for (size_t i = 1; i <= count; ++i) {
                    char key[32]; snprintf(key, sizeof(key), "%zu", i);
                    cJSON *v = cJSON_DetachItemFromObjectCaseSensitive(out, key);
                    if (!v || !cJSON_AddItemToArray(list, v)) { cJSON_Delete(v); cJSON_Delete(list); l->failed = 1; break; }
                }
                if (l->failed) break;
                cJSON_Delete(out); return list;
            }
            return out;
        }
        char key[128]; char *text = NULL; int numeric = 0; size_t index = 0;
        if (*l->p == '[') {
            ++l->p; lc_space(l);
            if (l->p < l->end && (*l->p == '"' || *l->p == '\'')) text = lc_string(l);
            else {
                const char *start = l->p;
                while (l->p < l->end && *l->p >= '0' && *l->p <= '9') {
                    if (index > 1000000) { l->failed = 1; break; }
                    index = index * 10 + (size_t)(*l->p++ - '0');
                }
                if (l->p == start || l->failed) break;
                snprintf(key, sizeof(key), "%zu", index); numeric = index > 0;
            }
            lc_space(l); if (l->p == l->end || *l->p++ != ']') { free(text); break; }
            lc_space(l); if (l->p == l->end || *l->p++ != '=') { free(text); break; }
        } else if (isalpha((unsigned char)*l->p) || *l->p == '_') {
            const char *start = l->p; size_t n = 0;
            while (l->p < l->end && (isalnum((unsigned char)*l->p) || *l->p == '_')) ++l->p;
            n = (size_t)(l->p - start); lc_space(l);
            if (l->p < l->end && *l->p == '=') {
                if (n >= sizeof(key)) break;
                memcpy(key, start, n); key[n] = 0; ++l->p;
            } else { l->p = start; index = next++; numeric = 1; snprintf(key, sizeof(key), "%zu", index); }
        } else { index = next++; numeric = 1; snprintf(key, sizeof(key), "%zu", index); }
        if (text) array = 0;
        else if (!numeric) array = 0;
        if (index > highest) highest = index;
        cJSON *v = lc_value(l);
        const char *name = text ? text : key;
        if (!v || cJSON_GetObjectItemCaseSensitive(out, name) || !cJSON_AddItemToObject(out, name, v)) {
            free(text); cJSON_Delete(v); break;
        }
        free(text); ++count; lc_space(l);
        if (l->p < l->end && (*l->p == ',' || *l->p == ';')) ++l->p;
        else if (l->p == l->end || *l->p != '}') break;
    }
    l->failed = 1; cJSON_Delete(out); return NULL;
}
static cJSON *lc_value(LuaCapture *l) {
    lc_space(l);
    if (++l->nodes > 300000 || l->p >= l->end || l->failed) { l->failed = 1; return NULL; }
    if (*l->p == '{') return lc_table(l);
    if (*l->p == '"' || *l->p == '\'') {
        char *s = lc_string(l); if (!s) return NULL;
        cJSON *v = cJSON_CreateString(s); free(s); return v;
    }
    const char *words[] = {"true", "false", "nil"};
    for (int i = 0; i < 3; ++i) {
        size_t n = strlen(words[i]);
        if ((size_t)(l->end - l->p) >= n && !strncmp(l->p, words[i], n) &&
            (l->p + n == l->end || (!isalnum((unsigned char)l->p[n]) && l->p[n] != '_'))) {
            l->p += n; return i == 2 ? cJSON_CreateNull() : cJSON_CreateBool(i == 0);
        }
    }
    const char *start = l->p;
    if (*l->p == '-') ++l->p;
    const char *digits = l->p;
    while (l->p < l->end && isdigit((unsigned char)*l->p)) ++l->p;
    if (l->p == digits) { l->failed = 1; return NULL; }
    if (l->p < l->end && *l->p == '.') { ++l->p; while (l->p < l->end && isdigit((unsigned char)*l->p)) ++l->p; }
    if (l->p < l->end && (*l->p == 'e' || *l->p == 'E')) {
        ++l->p; if (l->p < l->end && (*l->p == '+' || *l->p == '-')) ++l->p;
        digits = l->p; while (l->p < l->end && isdigit((unsigned char)*l->p)) ++l->p;
        if (l->p == digits) { l->failed = 1; return NULL; }
    }
    size_t n = (size_t)(l->p - start); char number[128];
    if (n >= sizeof(number)) { l->failed = 1; return NULL; }
    memcpy(number, start, n); number[n] = 0;
    double value = strtod(number, NULL);
    if (!isfinite(value)) { l->failed = 1; return NULL; }
    return cJSON_CreateNumber(value);
}
static cJSON *capture_lua(const char *raw, size_t n) {
    if (memchr(raw, 0, n)) return NULL;
    LuaCapture l = {raw, raw + n, 0, 0, 0};
    if (n >= 3 && !memcmp(raw, "\xef\xbb\xbf", 3)) l.p += 3;
    lc_space(&l); const char *name = "SimforeverProbeDB"; size_t len = strlen(name);
    if ((size_t)(l.end - l.p) < len || strncmp(l.p, name, len)) return NULL;
    l.p += len; lc_space(&l); if (l.p == l.end || *l.p++ != '=') return NULL;
    cJSON *root = lc_value(&l); lc_space(&l);
    if (l.p != l.end || l.failed || !cJSON_IsObject(root)) {
        fprintf(stderr, "SavedVariables parse stopped at byte %zu (depth %u, values %u).\n", (size_t)(l.p - raw), l.depth, l.nodes);
        cJSON_Delete(root); return NULL;
    }
    return root;
}
static char *capture_base64(const char *raw, size_t n) {
    static const char alphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    size_t size = 4 * ((n + 2) / 3); char *out = malloc(size + 1); if (!out) return NULL;
    size_t at = 0;
    for (size_t i = 0; i < n; i += 3) {
        unsigned x = (unsigned char)raw[i] << 16;
        if (i + 1 < n) x |= (unsigned char)raw[i + 1] << 8;
        if (i + 2 < n) x |= (unsigned char)raw[i + 2];
        out[at++] = alphabet[x >> 18]; out[at++] = alphabet[(x >> 12) & 63];
        out[at++] = i + 1 < n ? alphabet[(x >> 6) & 63] : '=';
        out[at++] = i + 2 < n ? alphabet[x & 63] : '=';
    }
    out[at] = 0; return out;
}
static char *capture_payload(const char *raw, size_t n, const char *source, char hash[65]) {
    if (n > CAPTURE_LIMIT / 2) return NULL;
    cJSON *root = capture_lua(raw, n); if (!root) return NULL;
    cJSON *version = cJSON_GetObjectItemCaseSensitive(root, "version");
    if (!cJSON_IsString(version) || !version->valuestring[0] || capture_hash_bytes(raw, n, hash)) { cJSON_Delete(root); return NULL; }
    cJSON *out = cJSON_CreateObject(), *evidence = cJSON_CreateObject();
    char *base64 = capture_base64(raw, n); char source_hash[65];
    if (!out || !evidence || !base64 || capture_hash_bytes(source, strlen(source), source_hash)) {
        cJSON_Delete(root); cJSON_Delete(out); cJSON_Delete(evidence); free(base64); return NULL;
    }
    cJSON_AddStringToObject(out, "version", version->valuestring);
    const char *keys[] = {"build", "character"};
    for (size_t i = 0; i < sizeof(keys) / sizeof(*keys); ++i) {
        cJSON *v = cJSON_GetObjectItemCaseSensitive(root, keys[i]);
        if (cJSON_IsObject(v)) cJSON_AddItemToObject(out, keys[i], cJSON_Duplicate(v, 1));
    }
    cJSON_AddStringToObject(evidence, "format", "simforever-savedvariables-1");
    cJSON_AddStringToObject(evidence, "sha256", hash);
    cJSON_AddStringToObject(evidence, "sourceId", source_hash);
    cJSON_AddStringToObject(evidence, "encoding", "base64");
    cJSON_AddStringToObject(evidence, "data", base64); free(base64);
    cJSON_AddItemToObject(out, "evidence", evidence);
    /* Nest the complete recording so the transitional analyzer does not fit
     * the same measurements again. The route keeps this JSON body verbatim. */
    cJSON_AddItemToObject(out, "capture", root);
    char *json = cJSON_PrintUnformatted(out); cJSON_Delete(out);
    if (json && strlen(json) > CAPTURE_LIMIT) { free(json); return NULL; }
    return json;
}

static char *capture_decode(const char *text, size_t *length) {
    static const char alphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    size_t n = strlen(text); if (n % 4 || n > CAPTURE_LIMIT) return NULL;
    char *out = malloc(n / 4 * 3 + 1); if (!out) return NULL;
    size_t at = 0;
    for (size_t i = 0; i < n; i += 4) {
        unsigned v[4] = {0}; int pad = 0;
        for (int j = 0; j < 4; ++j) {
            char c = text[i + (size_t)j]; const char *p = c ? strchr(alphabet, c) : NULL;
            if (c == '=' && j >= 2 && i + 4 == n) { ++pad; continue; }
            if (!p || pad) { free(out); return NULL; }
            v[j] = (unsigned)(p - alphabet);
        }
        if ((pad == 2 && (v[1] & 15)) || (pad == 1 && (v[2] & 3))) { free(out); return NULL; }
        unsigned bits = v[0] << 18 | v[1] << 12 | v[2] << 6 | v[3];
        out[at++] = (char)(bits >> 16);
        if (pad < 2) out[at++] = (char)(bits >> 8);
        if (!pad) out[at++] = (char)bits;
    }
    out[at] = 0; *length = at; return out;
}
static int capture_valid(const char *json, const char *name) {
    cJSON *root = cJSON_ParseWithLengthOpts(json, strlen(json) + 1, NULL, 1);
    cJSON *e = cJSON_GetObjectItemCaseSensitive(root, "evidence");
    cJSON *data = cJSON_GetObjectItemCaseSensitive(e, "data"), *hash = cJSON_GetObjectItemCaseSensitive(e, "sha256");
    cJSON *format = cJSON_GetObjectItemCaseSensitive(e, "format"), *encoding = cJSON_GetObjectItemCaseSensitive(e, "encoding");
    char computed[65]; size_t n = 0; char *raw = cJSON_IsString(data) ? capture_decode(data->valuestring, &n) : NULL;
    int valid = raw && cJSON_IsString(format) && !strcmp(format->valuestring, "simforever-savedvariables-1") &&
        cJSON_IsString(encoding) && !strcmp(encoding->valuestring, "base64") &&
        cJSON_IsString(hash) && strlen(hash->valuestring) == 64 &&
        !strncmp(name, hash->valuestring, 64) && !capture_hash_bytes(raw, n, computed) && !strcmp(computed, hash->valuestring);
    cJSON *parsed = valid ? capture_lua(raw, n) : NULL;
    valid = parsed && cJSON_Compare(parsed, cJSON_GetObjectItemCaseSensitive(root, "capture"), 1);
    const char *keys[] = {"version", "build", "character"};
    for (size_t i = 0; valid && i < sizeof(keys) / sizeof(*keys); ++i) {
        cJSON *a = cJSON_GetObjectItemCaseSensitive(parsed, keys[i]), *b = cJSON_GetObjectItemCaseSensitive(root, keys[i]);
        valid = (!a && !b) || cJSON_Compare(a, b, 1);
    }
    cJSON_Delete(parsed); cJSON_Delete(root); free(raw); return valid;
}

/* Each config owns its own evidence queue. Neither consent nor API preferences
 * are rewritten by this collector; the existing UI remains their owner. */
typedef struct {
    char config[PATH_CAP], queue[PATH_CAP], cursor[256]; time_t poll;
    int active, prompted, grant, quiet;
#ifdef _WIN32
    HANDLE lock;
#else
    int lock;
#endif
} CaptureCollector;

static cJSON *capture_config(const char *dir) {
    char path[PATH_CAP]; size_t n = 0;
    if (path_join(path, dir, "config.json")) return NULL;
    char *text = capture_read(path, &n); if (!text) return NULL;
    cJSON *root = cJSON_ParseWithLengthOpts(text, n + 1, NULL, 1); free(text);
    if (!cJSON_IsObject(root)) { cJSON_Delete(root); return NULL; }
    return root;
}
static const char *capture_text(cJSON *root, const char *key) {
    cJSON *v = cJSON_GetObjectItemCaseSensitive(root, key); return cJSON_IsString(v) ? v->valuestring : NULL;
}
static void capture_note(CaptureCollector *c, const char *message) {
    char path[PATH_CAP]; if (path_join(path, c->queue, "status.txt")) return;
    char text[512]; int n = snprintf(text, sizeof(text), "%lld %s\n", (long long)time(NULL), message);
    if (n > 0 && n < (int)sizeof(text)) (void)capture_atomic(path, text, (size_t)n);
    fprintf(stderr, "Recording collector: %s\n", message);
}
static int capture_default_config(char *out) {
#ifdef _WIN32
    wchar_t base[PATH_CAP]; char utf[PATH_CAP]; DWORD n = GetEnvironmentVariableW(L"APPDATA", base, PATH_CAP);
    if (!n || n >= PATH_CAP || !WideCharToMultiByte(CP_UTF8, 0, base, -1, utf, PATH_CAP, NULL, NULL)) return -1;
    return path_join(out, utf, "simforever-companion");
#elif defined(__APPLE__)
    const char *home = getenv("HOME"); if (!home) return -1;
    return path_join(out, home, "Library/Application Support/simforever-companion");
#else
    const char *base = getenv("XDG_CONFIG_HOME");
    if (base && base[0] == '/') return path_join(out, base, "simforever-companion");
    base = getenv("HOME"); return base ? path_join(out, base, ".config/simforever-companion") : -1;
#endif
}
static void capture_close(CaptureCollector *c) {
    if (!c->active) return;
#ifdef _WIN32
    CloseHandle(c->lock);
#else
    close(c->lock);
#endif
    c->active = 0;
}
static int capture_lock(CaptureCollector *c) {
    if (c->active) return 0;
    if (path_join(c->queue, c->config, "recordings") || mkdir_tree(c->queue)) return -1;
    char path[PATH_CAP]; if (path_join(path, c->queue, ".lock")) return -1;
#ifdef _WIN32
    wchar_t *w = wide_path(path);
    c->lock = w ? CreateFileW(w, GENERIC_READ | GENERIC_WRITE, 0, NULL, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL) : INVALID_HANDLE_VALUE;
    free(w); if (c->lock == INVALID_HANDLE_VALUE) return -1;
#else
    c->lock = open(path, O_RDWR | O_CREAT | O_NOFOLLOW, 0600);
    if (c->lock < 0) return -1;
    if (flock(c->lock, LOCK_EX | LOCK_NB)) { close(c->lock); return -1; }
#endif
    c->active = 1; return 0;
}
static int capture_init(CaptureCollector *c, int argc, char **argv) {
    memset(c, 0, sizeof(*c)); if (capture_default_config(c->config)) return 0;
    for (int i = 1; i < argc; ++i) {
        const char *p = argv[i]; if (*p != '-') break;
        ++p; if (*p == '-') ++p;
        if (!*p) break;
        size_t len = strcspn(p, "=");
        const char *skip[] = {"dry-run", "print-payload", "show", "version", "h", "help"};
        for (size_t j = 0; j < sizeof(skip) / sizeof(*skip); ++j)
            if (strlen(skip[j]) == len && !strncmp(p, skip[j], len)) return 0;
        const char *value = p[len] == '=' ? p + len + 1 : NULL;
        const char *names[] = {"config", "wow", "api", "game", "name", "interval"};
        for (size_t j = 0; j < sizeof(names) / sizeof(*names); ++j) {
            if (strlen(names[j]) != len || strncmp(p, names[j], len)) continue;
            if (!value && i + 1 < argc) value = argv[++i];
            if (!value) return 0;
            if (j == 0) {
                if (strlen(value) >= PATH_CAP || !*value) return 0;
                strcpy(c->config, value);
            }
            break;
        }
    }
    return 1;
}

#ifndef _WIN32
static size_t capture_receive(void *data, size_t width, size_t count, void *arg) {
    if (width && count > SIZE_MAX / width) return 0;
    size_t n = width * count; CaptureBuffer *b = arg;
    if (b->n + n > 65536 || cb_add(b, data, n)) return 0;
    return n;
}
#endif
static int capture_post(const char *url, const char *json, long *status) {
    int ok = 0; CaptureBuffer response = {0}; *status = 0;
#ifdef _WIN32
    wchar_t *w = wide(url); URL_COMPONENTS u = {0}; u.dwStructSize = sizeof(u);
    u.dwHostNameLength = u.dwUrlPathLength = (DWORD)-1;
    HINTERNET session = NULL, conn = NULL, req = NULL;
    if (!w || !WinHttpCrackUrl(w, 0, 0, &u) || u.nScheme != INTERNET_SCHEME_HTTPS || u.dwHostNameLength >= 256) goto done;
    wchar_t host[256]; memcpy(host, u.lpszHostName, u.dwHostNameLength * sizeof(wchar_t)); host[u.dwHostNameLength] = 0;
    session = WinHttpOpen(L"Simforever-Collector/1", WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY, NULL, NULL, 0);
    if (!session || !WinHttpSetTimeouts(session, 5000, 5000, 10000, 10000)) goto done;
    conn = WinHttpConnect(session, host, u.nPort, 0); if (!conn) goto done;
    req = WinHttpOpenRequest(conn, L"POST", u.lpszUrlPath, NULL, NULL, NULL, WINHTTP_FLAG_SECURE);
    DWORD policy = WINHTTP_OPTION_REDIRECT_POLICY_NEVER;
    if (!req || !WinHttpSetOption(req, WINHTTP_OPTION_REDIRECT_POLICY, &policy, sizeof(policy))) goto done;
    size_t n = strlen(json); if (n > CAPTURE_LIMIT) goto done;
    if (!WinHttpSendRequest(req, L"Content-Type: application/json\r\n", (DWORD)-1, (void *)json, (DWORD)n, (DWORD)n, 0) || !WinHttpReceiveResponse(req, NULL)) goto done;
    DWORD code = 0, length = sizeof(code);
    if (!WinHttpQueryHeaders(req, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER, NULL, &code, &length, NULL)) goto done;
    *status = (long)code;
    char buf[4096]; DWORD got; ULONGLONG deadline = GetTickCount64() + 15000;
    for (;;) {
        if (GetTickCount64() > deadline || !WinHttpReadData(req, buf, sizeof(buf), &got)) goto done;
        if (!got) break;
        if (response.n + got > 65536 || cb_add(&response, buf, got)) goto done;
    }
    ok = 1;
done:
    if (req) WinHttpCloseHandle(req);
    if (conn) WinHttpCloseHandle(conn);
    if (session) WinHttpCloseHandle(session);
    free(w);
#else
    CURL *curl = curl_easy_init();
    if (curl) {
        curl_easy_setopt(curl, CURLOPT_URL, url);
        curl_easy_setopt(curl, CURLOPT_PROTOCOLS_STR, "https");
        curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
        curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 5L);
        curl_easy_setopt(curl, CURLOPT_TIMEOUT, 20L);
#ifdef SF_CAPTURE_TEST
        /* Only fixture binaries can trust the disposable integration CA. */
        const char *ca = getenv("SF_CAPTURE_TEST_CA");
        if (ca) curl_easy_setopt(curl, CURLOPT_CAINFO, ca);
#endif
        curl_easy_setopt(curl, CURLOPT_USERAGENT, "Simforever-Collector/1");
        curl_easy_setopt(curl, CURLOPT_POSTFIELDS, json);
        curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE_LARGE, (curl_off_t)strlen(json));
        struct curl_slist *headers = curl_slist_append(NULL, "Content-Type: application/json");
        curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
        curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, capture_receive);
        curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response);
        if (curl_easy_perform(curl) == CURLE_OK && curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, status) == CURLE_OK) ok = 1;
        curl_slist_free_all(headers); curl_easy_cleanup(curl);
    }
#endif
    if (ok && *status >= 200 && *status < 300 && response.s) {
        cJSON *root = cJSON_ParseWithLengthOpts(response.s, response.n + 1, NULL, 1);
        cJSON *data = cJSON_GetObjectItemCaseSensitive(root, "data");
        cJSON *id = cJSON_GetObjectItemCaseSensitive(data, "uploadId");
        cJSON *error = cJSON_GetObjectItemCaseSensitive(root, "error");
        ok = cJSON_IsNumber(id) && id->valuedouble > 0 && (!error || cJSON_IsNull(error));
        cJSON_Delete(root);
    } else ok = 0;
    free(response.s); return ok ? 0 : -1;
}

static int capture_filename(const char *name) {
    if (strlen(name) != 69 || strcmp(name + 64, ".json")) return 0;
    for (int i = 0; i < 64; ++i) if (!isxdigit((unsigned char)name[i]) || (name[i] >= 'A' && name[i] <= 'F')) return 0;
    return 1;
}
typedef struct { char **names; size_t count, raw_count; uint64_t size; } CaptureFiles;
static void capture_files_free(CaptureFiles *f) {
    for (size_t i = 0; i < f->count; ++i) free(f->names[i]);
    free(f->names);
}
static int capture_files_add(CaptureFiles *f, const char *dir, const char *name) {
    int json = capture_filename(name);
    int raw = strlen(name) == 68 && !strcmp(name + 64, ".lua");
    if (raw) for (int i = 0; i < 64; ++i) if (!isxdigit((unsigned char)name[i])) raw = 0;
    if (!json && !raw) return 0;
    char path[PATH_CAP]; if (path_join(path, dir, name)) return -1;
    FILE *file = open_file(path, "rb"); if (!file) return 0;
    uint64_t size = 0; int rc = file_size(file, &size); fclose(file); if (rc) return -1;
    f->size += size;
    if (!json) { ++f->raw_count; return 0; }
    if (f->count >= 4096) return -1;
    char **names = realloc(f->names, (f->count + 1) * sizeof(*names)); if (!names) return -1;
    f->names = names; names[f->count] = malloc(strlen(name) + 1); if (!names[f->count]) return -1;
    strcpy(names[f->count++], name); return 0;
}
static int capture_name_compare(const void *a, const void *b) { return strcmp(*(char *const *)a, *(char *const *)b); }
static int capture_files(const char *dir, CaptureFiles *out) {
    memset(out, 0, sizeof(*out)); int rc = 0;
#ifdef _WIN32
    char pattern[PATH_CAP]; if (path_join(pattern, dir, "*")) return -1;
    wchar_t *w = wide_path(pattern); WIN32_FIND_DATAW data;
    HANDLE find = w ? FindFirstFileW(w, &data) : INVALID_HANDLE_VALUE; free(w);
    if (find == INVALID_HANDLE_VALUE) return -1;
    do {
        char name[512];
        if (!(data.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) && WideCharToMultiByte(CP_UTF8, 0, data.cFileName, -1, name, sizeof(name), NULL, NULL) && capture_files_add(out, dir, name)) { rc = -1; break; }
    } while (FindNextFileW(find, &data));
    FindClose(find);
#else
    DIR *d = opendir(dir); if (!d) return -1;
    struct dirent *entry;
    while ((entry = readdir(d))) if (capture_files_add(out, dir, entry->d_name)) { rc = -1; break; }
    closedir(d);
#endif
    if (out->count) qsort(out->names, out->count, sizeof(*out->names), capture_name_compare);
    return rc;
}
static int capture_enqueue(CaptureCollector *c, const char *source, uint64_t *used) {
    size_t n = 0, check_n = 0; char *raw = capture_read_limit(source, &n, CAPTURE_SOURCE_LIMIT); if (!raw) return -1;
    char *check = capture_read_limit(source, &check_n, CAPTURE_SOURCE_LIMIT);
    if (!check || n != check_n || memcmp(raw, check, n)) { free(raw); free(check); return -1; }
    free(check); char hash[65];
    if (capture_hash_bytes(raw, n, hash)) { free(raw); return -1; }
    char *json = capture_payload(raw, n, source, hash);
    const char *data = json ? json : raw; size_t size = json ? strlen(json) : n;
    char name[80], path[PATH_CAP]; snprintf(name, sizeof(name), "%s.%s", hash, json ? "json" : "lua");
    if (path_join(path, c->queue, name)) { free(raw); free(json); return -1; }
    FILE *existing = open_file(path, "rb");
    if (existing) { fclose(existing); free(raw); free(json); return 0; }
    if (*used > CAPTURE_QUOTA || size > CAPTURE_QUOTA - *used) {
        capture_note(c, "Recording archive is full; pending evidence was retained. Archive acknowledged files to free space.");
        free(raw); free(json); return -1;
    }
    int rc = capture_atomic(path, data, size);
    if (!rc) {
        *used += size;
        capture_note(c, json ? "Complete recording saved locally; awaiting upload acknowledgement." :
            "Original recording retained locally; its format or size needs conversion before upload.");
    }
    free(raw); free(json);
    return rc;
}
static int capture_url(cJSON *cfg, char out[4096]) {
    const char *api = capture_text(cfg, "apiBase"), *game = capture_text(cfg, "game");
    if (!api || !*api) api = SF_ORIGIN;
    if (!game || !*game || !strcmp(game, "classic_era")) game = "forever";
    if (strncmp(api, "https://", 8) || strchr(api, '?') || strchr(api, '#') || strchr(api, '@')) return -1;
    for (const char *p = game; *p; ++p) if (!isalnum((unsigned char)*p) && *p != '_' && *p != '-') return -1;
    size_t n = strlen(api); while (n && api[n - 1] == '/') --n;
    if (n > 3500) return -1;
    int size = snprintf(out, 4096, "%.*s/v1/games/%s/probe", (int)n, api, game);
    return size > 0 && size < 4096 ? 0 : -1;
}
static int capture_ack_path(CaptureCollector *c, const char *name, const char *url, char *out) {
    char hash[65], file[160];
    if (capture_hash_bytes(url, strlen(url), hash)) return -1;
    snprintf(file, sizeof(file), "%.64s-%s.ack", name, hash);
    return path_join(out, c->queue, file);
}
static const char capture_notice[] =
    "Share complete Simforever recordings?\n\n"
    "This includes your character's measurements, game/addon versions, item readings, loot sources, "
    "and the group members or inspected players recorded by the addon, including names and gear. "
    "The original addon recording is retained alongside its parsed contents.\n\n"
    "This is additional to the existing measurement and combat-log uploads. "
    "Declining keeps complete recordings on this computer. The existing uploader keeps its own settings.\n\n"
    "You can inspect a recording without uploading with --capture-probe INPUT OUTPUT.\n\n"
    "Send complete recordings to ";
static int capture_authorize(CaptureCollector *c, const char *url) {
    char path[PATH_CAP]; if (path_join(path, c->config, "capture-consent.json")) return 0;
    size_t n = 0; char *text = capture_read(path, &n);
    cJSON *saved = text ? cJSON_ParseWithLengthOpts(text, n + 1, NULL, 1) : NULL; free(text);
    const char *endpoint = capture_text(saved, "endpoint");
    if (!c->grant && endpoint && !strcmp(endpoint, url)) {
        int allowed = cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(saved, "approved"));
        cJSON_Delete(saved); return allowed;
    }
    cJSON_Delete(saved);
    if (!c->grant && (c->prompted || c->quiet)) return 0;
    c->prompted = 1; int allowed = c->grant;
    if (!allowed) {
#ifdef _WIN32
        char message[5000]; snprintf(message, sizeof(message), "%s%s ?", capture_notice, url);
        wchar_t *w = wide(message);
        if (w) allowed = MessageBoxW(NULL, w, L"Simforever recording collection", MB_YESNO | MB_ICONQUESTION | MB_DEFBUTTON2) == IDYES;
        free(w);
#else
        if (isatty(STDIN_FILENO)) {
            fprintf(stderr, "%s%s ? [y/N]: ", capture_notice, url);
            char answer[32]; allowed = fgets(answer, sizeof(answer), stdin) && (!strcmp(answer, "y\n") || !strcmp(answer, "yes\n"));
        } else {
            capture_note(c, "Complete recordings retained locally. Read the collection notice and use --collect-yes to enable their upload.");
            return 0;
        }
#endif
    }
    cJSON *consent = cJSON_CreateObject();
    cJSON_AddStringToObject(consent, "endpoint", url);
    cJSON_AddBoolToObject(consent, "approved", allowed);
    cJSON_AddStringToObject(consent, "notice", "complete-recordings-1");
    cJSON_AddNumberToObject(consent, "at", (double)time(NULL));
    char *json = cJSON_PrintUnformatted(consent); cJSON_Delete(consent);
    int rc = json ? capture_atomic(path, json, strlen(json)) : -1; free(json);
    c->grant = 0; return !rc && allowed;
}
static int capture_send_one(CaptureCollector *c, CaptureFiles *files, const char *url, int (*post)(const char *, const char *, long *)) {
    size_t start = 0;
    while (start < files->count && strcmp(files->names[start], c->cursor) <= 0) ++start;
    for (size_t k = 0; k < files->count; ++k) {
        const char *name = files->names[(start + k) % files->count]; char ack[PATH_CAP], path[PATH_CAP];
        if (capture_ack_path(c, name, url, ack) || path_join(path, c->queue, name)) continue;
        FILE *f = open_file(ack, "rb"); if (f) { fclose(f); continue; }
        strcpy(c->cursor, name); size_t n = 0; char *json = capture_read(path, &n); if (!json) continue;
        if (!capture_valid(json, name)) {
            free(json); capture_note(c, "Queued recording failed its integrity check; retained locally and not uploaded.");
            continue;
        }
        long status = 0; int rc = post(url, json, &status); free(json);
        if (!rc) {
            if (capture_atomic(ack, "acknowledged\n", 13)) return -1;
            capture_note(c, "Complete recording acknowledged by the platform; local evidence retained.");
        } else {
            capture_note(c, "Upload unavailable or rejected; complete recording remains queued locally.");
            if (status == 429) c->poll = time(NULL) + 60;
        }
        return rc;
    }
    return 0;
}
static void capture_tick(CaptureCollector *c, int (*post)(const char *, const char *, long *)) {
    if (time(NULL) < c->poll) return;
    c->poll = time(NULL) + 15;
    cJSON *cfg = capture_config(c->config); if (!cfg) return;
    if (!cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "consentGranted"))) { cJSON_Delete(cfg); return; }
    if (capture_lock(c)) { cJSON_Delete(cfg); return; }
    CaptureFiles files;
    if (capture_files(c->queue, &files)) { capture_files_free(&files); cJSON_Delete(cfg); return; }
    uint64_t used = files.size; capture_files_free(&files);
    cJSON *paths = cJSON_GetObjectItemCaseSensitive(cfg, "watch"), *p;
    cJSON_ArrayForEach(p, paths) if (cJSON_IsString(p)) (void)capture_enqueue(c, p->valuestring, &used);
    char url[4096];
    if (!capture_url(cfg, url) && capture_authorize(c, url)) {
        if (!capture_files(c->queue, &files)) (void)capture_send_one(c, &files, url, post);
        capture_files_free(&files);
    }
    cJSON_Delete(cfg);
}
static int capture_export(const char *source, const char *out) {
    size_t n = 0; char *raw = capture_read(source, &n); if (!raw) return 1;
    char hash[65]; char *json = capture_payload(raw, n, source, hash); free(raw);
    if (!json) { fprintf(stderr, "Cannot parse or preserve this recording within the upload limit.\n"); return 1; }
    int rc = capture_atomic(out, json, strlen(json)); free(json);
    return rc ? 1 : 0;
}
#endif
