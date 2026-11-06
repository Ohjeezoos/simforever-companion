/* Combat logs from C on Linux and macOS, as the Windows host sends them. The
 * launcher's loop calls logs_tick() every second and it works once a minute;
 * the legacy worker then runs with -logs=false and never sends logs itself. */
#ifndef SF_LOGS_POSIX_H
#define SF_LOGS_POSIX_H
#include <glob.h>

#define LOGS_QUIET_SECONDS 900
/* The most one upload carries, ending on a line: a long log goes up in parts
 * that each finish well inside the two minute request limits on both ends. */
#define LOGS_PART UINT64_C(16777216)
#define LOGS_BOUNDARY "simforever-companion-log-7d3f2a9b"

typedef struct LogsFile { char *path; uint64_t size; time_t modified; char hash[65]; struct LogsFile *next; } LogsFile;
typedef struct { char config[PATH_CAP], wow[PATH_CAP]; int active; time_t next_poll, next_upload; LogsFile *files; } LogsSender;

/* A -logs=false the player gives keeps the logs on the machine. */
static int logs_flag_off(int argc, char **argv) {
    for (int i = 1; i < argc; ++i) {
        const char *a = argv[i]; if (*a != '-') continue; ++a; if (*a == '-') ++a;
        if (!strcmp(a, "logs=false") || !strcmp(a, "logs=0") ||
            (!strcmp(a, "logs") && i + 1 < argc && (!strcmp(argv[i + 1], "false") || !strcmp(argv[i + 1], "0")))) return 1;
    }
    return 0;
}
static int logs_init(LogsSender *s, int argc, char **argv) {
    memset(s, 0, sizeof(*s)); AddonManager a;
    if (logs_flag_off(argc, argv) || !addon_init(&a, argc, argv)) return 0;
    strcpy(s->config, a.config); strcpy(s->wow, a.explicit_wow); s->active = 1; return 1;
}
static void logs_free(LogsSender *s) {
    while (s->files) { LogsFile *f = s->files; s->files = f->next; free(f->path); free(f); }
}
/* The game folder holding Logs, from a root the way the addon is placed. */
static int logs_game(const char *root, char *out) {
    if (!root || !*root || strlen(root) >= PATH_CAP) return -1;
    strcpy(out, root); size_t n = strlen(out); while (n > 1 && out[n - 1] == '/') out[--n] = 0;
    if (addon_is_forever(out)) return 0;
    const char *inside[] = {"/_forever_/", "/_classic_beta_/"};
    for (size_t i = 0; i < sizeof(inside) / sizeof(*inside); ++i) {
        char *part = strstr(out, inside[i]); if (part) { part[strlen(inside[i]) - 1] = 0; return 0; }
    }
    char *child = malloc(PATH_CAP); if (!child) return -1;
    const char *flavors[] = {"_forever_", "_classic_beta_"};
    for (size_t i = 0; i < sizeof(flavors) / sizeof(*flavors); ++i)
        if (!addon_join(child, out, flavors[i]) && addon_kind(child) == 2) { strcpy(out, child); free(child); return 0; }
    free(child); return -1;
}
/* Where a log goes: the Simforever service, which takes appends, or a custom
 * server's own log route, which takes the whole file. */
static int logs_endpoint(cJSON *cfg, char url[4096], int *custom, char display[128]) {
    const char *api = capture_text(cfg, "apiBase"), *game = capture_text(cfg, "game"), *name = capture_text(cfg, "displayName");
    display[0] = 0; if (name && strlen(name) < 100 && !strpbrk(name, "\r\n")) strcpy(display, name);
    size_t n = api ? strlen(api) : 0; while (n && api[n - 1] == '/') --n;
    const char *origin = SF_ORIGIN;
#ifdef SF_LOGS_TEST
    /* Only fixture binaries may name a local service. */
    if (getenv("SF_LOGS_TEST_ORIGIN")) origin = getenv("SF_LOGS_TEST_ORIGIN");
#endif
    if (!api || !*api || (n == strlen(SF_ORIGIN) && !memcmp(api, SF_ORIGIN, n))) {
        *custom = 0; return snprintf(url, 4096, "%s/contributor-api/logs/anonymous", origin) < 4096 ? 0 : -1;
    }
    *custom = 1;
    if (!game || !*game || !strcmp(game, "classic_era")) game = "forever";
    if (strncmp(api, "https://", 8) || strchr(api, '?') || strchr(api, '#') || strchr(api, '@') || n > 3500) return -1;
    for (const char *q = game; *q; ++q) if (!isalnum((unsigned char)*q) && *q != '_' && *q != '-') return -1;
    return snprintf(url, 4096, "%.*s/v1/games/%s/logs", (int)n, api, game) < 4096 ? 0 : -1;
}
static size_t logs_read(char *buffer, size_t size, size_t count, void *file) { return fread(buffer, size, count, (FILE *)file); }
static cJSON *logs_post(const char *url, FILE *body, uint64_t size, const char *type, const char *base, const char *display, long *status) {
    *status = 0; CaptureBuffer response = {0}; cJSON *result = NULL; char line[256];
    CURL *curl = curl_easy_init(); if (!curl) return NULL;
    struct curl_slist *headers = NULL;
    snprintf(line, sizeof(line), "Content-Type: %s", type); headers = curl_slist_append(headers, line);
    headers = curl_slist_append(headers, "Expect:");
    if (base) { snprintf(line, sizeof(line), "X-Simforever-Log-Base: %s", base); headers = curl_slist_append(headers, line); }
    if (display && *display) { snprintf(line, sizeof(line), "X-Simforever-Display-Name: %s", display); headers = curl_slist_append(headers, line); }
    curl_easy_setopt(curl, CURLOPT_URL, url);
#ifdef SF_LOGS_TEST
    curl_easy_setopt(curl, CURLOPT_PROTOCOLS_STR, getenv("SF_LOGS_TEST_ORIGIN") ? "http,https" : "https");
#else
    curl_easy_setopt(curl, CURLOPT_PROTOCOLS_STR, "https");
#endif
    curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
    curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 5L);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 120L);
    curl_easy_setopt(curl, CURLOPT_USERAGENT, "Simforever-Contributor/1");
    curl_easy_setopt(curl, CURLOPT_POST, 1L);
    curl_easy_setopt(curl, CURLOPT_READFUNCTION, logs_read);
    curl_easy_setopt(curl, CURLOPT_READDATA, body);
    curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE_LARGE, (curl_off_t)size);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, capture_receive);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response);
    rewind(body);
    if (curl_easy_perform(curl) == CURLE_OK && curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, status) == CURLE_OK && response.s)
        result = cJSON_ParseWithLengthOpts(response.s, response.n + 1, NULL, 1);
    curl_slist_free_all(headers); curl_easy_cleanup(curl); free(response.s); return result;
}
/* Receipts and progress share the Windows host's names, keyed by account. */
static int logs_receipt(const char *dir, const char *owner, const char *digest, char *path) {
    char who[65], name[160]; if (capture_hash_bytes(owner, strlen(owner), who)) return -1;
    snprintf(name, sizeof(name), "contribution-%s-%s.json", who, digest); return addon_join(path, dir, name);
}
static int logs_progress_path(const char *dir, const char *owner, const char *log_name, char *path) {
    char who[65], which[65], name[200];
    if (capture_hash_bytes(owner, strlen(owner), who) || capture_hash_bytes(log_name, strlen(log_name), which)) return -1;
    snprintf(name, sizeof(name), "contribution-%s-log-%s.progress.json", who, which); return addon_join(path, dir, name);
}
static int logs_progress(const char *path, uint64_t *size, char sha[65]) {
    size_t n = 0; char *text = capture_read(path, &n); cJSON *j = text ? cJSON_ParseWithLength(text, n) : NULL; free(text);
    cJSON *s = cJSON_GetObjectItemCaseSensitive(j, "size"); const char *h = capture_text(j, "sha256"); int ok = 0;
    if (cJSON_IsNumber(s) && s->valuedouble > 0 && s->valuedouble < 9007199254740991.0 && h && strlen(h) == 64 && strspn(h, "0123456789abcdef") == 64) {
        *size = (uint64_t)s->valuedouble; strcpy(sha, h); ok = 1;
    }
    cJSON_Delete(j); return ok ? 0 : -1;
}
static void logs_progress_save(const char *path, uint64_t size, const char *sha) {
    char note[160]; int n = snprintf(note, sizeof(note), "{\"size\":%llu,\"sha256\":\"%s\"}", (unsigned long long)size, sha);
    if (n > 0 && n < (int)sizeof(note)) capture_atomic(path, note, (size_t)n);
}
/* Logs the legacy worker already sent from this config are known by hash. */
static int logs_legacy_sent(const char *dir, const char *digest) {
    char *path = malloc(PATH_CAP); size_t n = 0; char *text = path && !addon_join(path, dir, "state.json") ? capture_read(path, &n) : NULL; free(path);
    cJSON *state = text ? cJSON_ParseWithLength(text, n) : NULL, *entry; free(text); int sent = 0;
    cJSON_ArrayForEach(entry, cJSON_GetObjectItemCaseSensitive(state, "entries")) {
        const char *hash = capture_text(entry, "hash");
        if (entry->string && !strncmp(entry->string, "log:", 4) && hash && !strcmp(hash, digest)) { sent = 1; break; }
    }
    cJSON_Delete(state); return sent;
}
/* The whole file's SHA-256, kept while its size and time stand still. */
static const char *logs_hash(LogsSender *s, const char *path, const struct stat *st) {
    LogsFile *f = s->files; while (f && strcmp(f->path, path)) f = f->next;
    if (f && f->hash[0] && f->size == (uint64_t)st->st_size && f->modified == st->st_mtime) return f->hash;
    if (!f) {
        f = calloc(1, sizeof(*f)); char *copy = malloc(strlen(path) + 1);
        if (!f || !copy) { free(f); free(copy); return NULL; }
        strcpy(copy, path); f->path = copy; f->next = s->files; s->files = f;
    }
    f->hash[0] = 0; FILE *in = fopen(path, "rb"); unsigned char d[32]; struct stat after;
    if (!in) return NULL;
    int rc = sha256(in, (uint64_t)st->st_size, d); fclose(in);
    if (rc || stat(path, &after) || after.st_size != st->st_size || after.st_mtime != st->st_mtime) return NULL;
    f->size = (uint64_t)st->st_size; f->modified = st->st_mtime; hex_digest(d, f->hash); return f->hash;
}
/* One log: the bytes after the acknowledged prefix as an append, or for a
 * custom server the whole file. Returns 1 when the log is done. */
static int logs_send_one(LogsSender *s, const char *path, const char *name, uint64_t size, const char *digest,
                         const char *url, int custom, const char *display, const char *receipt, long *status) {
    const char *owner = custom ? url : "anonymous"; int done = 0;
    char *progress = malloc(PATH_CAP); FILE *in = progress ? fopen(path, "rb") : NULL, *spool = in ? tmpfile() : NULL;
    if (!progress || !in || !spool || logs_progress_path(s->config, owner, name, progress)) { if (in) fclose(in); if (spool) fclose(spool); free(progress); return 0; }
    char base[65] = "", expected[65], buf[65536]; unsigned char d[32]; uint64_t start = 0, end = size, extra = 0; int ok = 1;
    if (custom) {
        if (size > 256u * 1024u * 1024u || strpbrk(name, "\"\r\n")) ok = 0;
        int hn = ok ? fprintf(spool, "--" LOGS_BOUNDARY "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\nContent-Type: application/octet-stream\r\n\r\n", name) : -1;
        if (hn <= 0) ok = 0; else extra = (uint64_t)hn;
    } else {
        char copied[65];
        if (!logs_progress(progress, &start, base) && start < size && !sha256(in, start, d)) { hex_digest(d, copied); if (strcmp(copied, base)) { start = 0; base[0] = 0; } }
        else { start = 0; base[0] = 0; }
        if (end - start > LOGS_PART) {
            end = 0; uint64_t at = start + LOGS_PART;
            while (!end && at > start) {
                uint64_t from = at - start > sizeof(buf) ? at - sizeof(buf) : start; size_t n = (size_t)(at - from);
                if (fseeko(in, (off_t)from, SEEK_SET) || fread(buf, 1, n, in) != n) { ok = 0; break; }
                for (size_t i = n; i > 0; --i) if (buf[i - 1] == '\n') { end = from + i; break; }
                at = from;
            }
            if (!end) ok = 0;
        }
    }
    uint64_t left = end - start;
    if (ok && fseeko(in, (off_t)start, SEEK_SET)) ok = 0;
    while (ok && left) { size_t n = left > sizeof(buf) ? sizeof(buf) : (size_t)left; if (fread(buf, 1, n, in) != n || fwrite(buf, 1, n, spool) != n) { ok = 0; break; } left -= n; }
    if (ok && custom) { const char *tail = "\r\n--" LOGS_BOUNDARY "--\r\n"; if (fputs(tail, spool) < 0) ok = 0; else extra += strlen(tail); }
    if (ok && end == size) strcpy(expected, digest);
    else if (ok) { rewind(in); if (sha256(in, end, d)) ok = 0; else hex_digest(d, expected); }
    fclose(in);
    if (ok && !fflush(spool)) {
        cJSON *reply = logs_post(url, spool, end - start + extra, custom ? "multipart/form-data; boundary=" LOGS_BOUNDARY : "application/octet-stream",
                                 base[0] ? base : NULL, custom ? display : NULL, status);
        const char *hash = capture_text(custom ? cJSON_GetObjectItemCaseSensitive(reply, "data") : reply, "sha256");
        if (reply && *status >= 200 && *status < 300 && hash && !strcmp(hash, expected)) {
            if (!custom) logs_progress_save(progress, end, expected);
            if (end == size) { char *json = cJSON_PrintUnformatted(reply); if (json && !capture_atomic(receipt, json, strlen(json))) done = 1; free(json); }
        } else if (*status == 409) remove(progress); /* the service no longer knows the prefix: next time, whole */
        cJSON_Delete(reply);
    }
    fclose(spool); free(progress); return done;
}
static void logs_tick(LogsSender *s) {
    time_t now = time(NULL); if (!s->active || now < s->next_poll) return; s->next_poll = now + 60;
    cJSON *cfg = capture_config(s->config); cJSON *share = cJSON_GetObjectItemCaseSensitive(cfg, "shareCombatLogs");
    char url[4096], display[128]; int custom = 0;
    const char *root = s->wow[0] ? s->wow : capture_text(cfg, "wowPath");
    char *game = malloc(PATH_CAP), *pattern = malloc(PATH_CAP), *receipt = malloc(PATH_CAP);
    if (!game || !pattern || !receipt || !cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "consentGranted")) || (share && !cJSON_IsTrue(share)) ||
        logs_endpoint(cfg, url, &custom, display) || logs_game(root, game) || addon_join(pattern, game, "Logs/WoWCombatLog*.txt")) {
        cJSON_Delete(cfg); free(game); free(pattern); free(receipt); return;
    }
    cJSON_Delete(cfg);
    glob_t found = {0}; int sent = 0;
    if (!glob(pattern, 0, NULL, &found)) for (size_t i = 0; i < found.gl_pathc; ++i) {
        const char *path = found.gl_pathv[i], *name = strrchr(path, '/'); name = name ? name + 1 : path;
        struct stat st; if (stat(path, &st) || !S_ISREG(st.st_mode) || st.st_size <= 0) continue;
        /* A log is sent once it has gone unchanged for 15 minutes. */
        if (now - st.st_mtime < LOGS_QUIET_SECONDS) continue;
        const char *digest = logs_hash(s, path, &st);
        if (!digest || logs_receipt(s->config, custom ? url : "anonymous", digest, receipt) || addon_kind(receipt) == 1) continue;
        if (logs_legacy_sent(s->config, digest)) {
            char note[160]; snprintf(note, sizeof(note), "{\"sha256\":\"%s\",\"status\":\"sent_by_legacy_uploader\"}", digest);
            capture_atomic(receipt, note, strlen(note)); continue;
        }
        if (sent || now < s->next_upload) continue;
        long status = 0; logs_send_one(s, path, name, (uint64_t)st.st_size, digest, url, custom, display, receipt, &status);
        sent = 1; s->next_upload = now + (status == 429 ? 600 : 15);
    }
    globfree(&found); free(game); free(pattern); free(receipt);
}
#endif
