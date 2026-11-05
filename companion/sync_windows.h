/* Sync status uses the uploader's receipts and the current file contents. */
#ifndef SF_SYNC_WINDOWS_H
#define SF_SYNC_WINDOWS_H
enum { SF_SYNC_PAUSED, SF_SYNC_READY, SF_SYNC_PENDING, SF_SYNC_DONE };
typedef struct SyncHash { char *path; uint64_t size, modified; char hash[65]; struct SyncHash *next; } SyncHash;
typedef struct { SyncHash *files; } SyncCache;
static void sync_cache_free(SyncCache *cache) {
    while (cache->files) { SyncHash *file = cache->files; cache->files = file->next; free(file->path); free(file); }
}
static int sync_info(const char *path, uint64_t *size, uint64_t *modified) {
    wchar_t *w = wide_path(path); WIN32_FILE_ATTRIBUTE_DATA info = {0};
    int ok = w && GetFileAttributesExW(w, GetFileExInfoStandard, &info); free(w);
    if (!ok || (info.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY)) return -1;
    *size = ((uint64_t)info.nFileSizeHigh << 32) | info.nFileSizeLow;
    *modified = ((uint64_t)info.ftLastWriteTime.dwHighDateTime << 32) | info.ftLastWriteTime.dwLowDateTime;
    return 0;
}
static const char *sync_hash(SyncCache *cache, const char *path) {
    uint64_t size = 0, modified = 0; if (sync_info(path, &size, &modified)) return NULL;
    SyncHash *entry = cache->files;
    while (entry && strcmp(entry->path, path)) entry = entry->next;
    if (entry && entry->hash[0] && size == entry->size && modified == entry->modified) return entry->hash;
    if (!entry) {
        entry = calloc(1, sizeof(*entry)); if (!entry) return NULL;
        entry->path = malloc(strlen(path) + 1); if (!entry->path) { free(entry); return NULL; }
        strcpy(entry->path, path); entry->next = cache->files; cache->files = entry;
    }
    entry->hash[0] = 0;
    FILE *f = open_file(path, "rb"); unsigned char digest[32]; if (!f) return NULL;
    int rc = sha256(f, size, digest); fclose(f);
    uint64_t after_size = 0, after_modified = 0;
    if (rc || sync_info(path, &after_size, &after_modified) || after_size != size || after_modified != modified) return NULL;
    entry->size = size; entry->modified = modified; hex_digest(digest, entry->hash); return entry->hash;
}
static cJSON *sync_json(const char *dir, const char *name) {
    char *path = malloc(PATH_CAP); size_t size = 0;
    char *text = path && !addon_join(path, dir, name) ? capture_read(path, &size) : NULL; free(path);
    cJSON *json = text ? cJSON_ParseWithLengthOpts(text, size + 1, NULL, 1) : NULL; free(text); return json;
}
static uint64_t sync_uploaded_at(const char *text) {
    if (!text) return 0;
    int year, month, day, hour, minute, end = 0; double second;
    if (sscanf(text, "%d-%d-%dT%d:%d:%lf%n", &year, &month, &day, &hour, &minute, &second, &end) != 6 ||
        year < 1601 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59 || !isfinite(second) || second < 0 || second >= 60) return 0;
    SYSTEMTIME system = {0}; system.wYear = (WORD)year; system.wMonth = (WORD)month; system.wDay = (WORD)day;
    system.wHour = (WORD)hour; system.wMinute = (WORD)minute; system.wSecond = (WORD)second;
    FILETIME time; if (!SystemTimeToFileTime(&system, &time)) return 0;
    int offset = 0;
    if (text[end] == '+' || text[end] == '-') {
        int hours, minutes, consumed = 0;
        if (sscanf(text + end + 1, "%2d:%2d%n", &hours, &minutes, &consumed) != 2 || consumed != 5 || text[end + 1 + consumed] || hours < 0 || hours > 23 || minutes < 0 || minutes > 59) return 0;
        offset = (hours * 60 + minutes) * 60 * (text[end] == '+' ? 1 : -1);
    } else if (text[end] != 'Z' || text[end + 1]) return 0;
    uint64_t ticks = ((uint64_t)time.dwHighDateTime << 32) | time.dwLowDateTime;
    int64_t adjusted = (int64_t)ticks + (int64_t)((second - (int)second) * 10000000) - (int64_t)offset * 10000000;
    return adjusted > 0 ? (uint64_t)adjusted : 0;
}
static int sync_is_log(const char *path) {
    FILE *f = open_file(path, "rb"); if (!f) return 0;
    char head[513]; size_t n = fread(head, 1, sizeof(head) - 1, f); fclose(f); head[n] = 0;
    char *line = strchr(head, '\n'); if (line) *line = 0;
    char *event = strstr(head, "  "); if (!event) return 0; *event = 0;
    unsigned slash = 0, colon = 0;
    for (char *p = head; *p; ++p) { slash += *p == '/'; colon += *p == ':'; }
    return slash >= 1 && colon >= 2;
}
/* `linked` selects the legacy uploader's linked state; `c_logs` means C sends combat logs. */
static int sync_state_mode(SyncCache *cache, const char *dir, int linked, int c_logs) {
    cJSON *cfg = capture_config(dir);
    if (!cfg) return SF_SYNC_READY;
    if (!cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "consentGranted"))) { cJSON_Delete(cfg); return SF_SYNC_PAUSED; }
    cJSON *state = sync_json(dir, linked ? "legacy-linked/state.json" : "state.json"), *entries = cJSON_GetObjectItemCaseSensitive(state, "entries");
    cJSON *consent = sync_json(dir, "capture-consent.json"); char url[4096];
    const char *endpoint = capture_text(consent, "endpoint");
    int full = !capture_url(cfg, url) && endpoint && !strcmp(endpoint, url) && cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(consent, "approved"));
    CaptureCollector *collector = calloc(1, sizeof(*collector)); char *path = malloc(PATH_CAP);
    if (!collector || !path || addon_join(collector->queue, dir, "recordings")) {
        free(collector); free(path); cJSON_Delete(consent); cJSON_Delete(state); cJSON_Delete(cfg); return SF_SYNC_PENDING;
    }
    unsigned pending = 0, acknowledged = 0; cJSON *file;
    cJSON_ArrayForEach(file, cJSON_GetObjectItemCaseSensitive(cfg, "watch")) {
        if (!cJSON_IsString(file)) continue;
        uint64_t size, modified;
        if (sync_info(file->valuestring, &size, &modified) || !size) continue;
        if (full) {
            const char *hash = sync_hash(cache, file->valuestring); char name[80];
            if (!hash) { ++pending; continue; }
            snprintf(name, sizeof(name), "%s.json", hash);
            if (!capture_ack_path(collector, name, url, path) && addon_kind(path) == 1) ++acknowledged; else ++pending;
        } else {
            int seen = 0; cJSON *entry;
            cJSON_ArrayForEach(entry, entries) {
                const char *source = capture_text(entry, "path"), *hash = capture_text(entry, "hash");
                if (source && !strcmp(source, file->valuestring) && hash && strlen(hash) == 64 && sync_uploaded_at(capture_text(entry, "uploadedAt")) >= modified) { seen = 1; break; }
            }
            if (seen) ++acknowledged; else ++pending;
        }
    }
    if (full && addon_kind(collector->queue) == 2) {
        CaptureFiles queued;
        if (capture_files(collector->queue, &queued)) ++pending;
        else { pending += (unsigned)queued.raw_count; for (size_t i = 0; i < queued.count; ++i) {
            if (!capture_ack_path(collector, queued.names[i], url, path) && addon_kind(path) == 1) ++acknowledged; else ++pending;
        }
        }
        capture_files_free(&queued);
    }
    if (!c_logs && !cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(cfg, "skipCombatLogs"))) {
        cJSON_ArrayForEach(file, cJSON_GetObjectItemCaseSensitive(cfg, "watchLogs")) {
            if (!cJSON_IsString(file) || !sync_is_log(file->valuestring)) continue;
            size_t n = strlen(file->valuestring); char *key = malloc(n + 5);
            if (!key) { ++pending; continue; }
            memcpy(key, "log:", 4); memcpy(key + 4, file->valuestring, n + 1);
            cJSON *receipt = cJSON_GetObjectItemCaseSensitive(entries, key); free(key);
            const char *uploaded = capture_text(receipt, "hash");
            if (!uploaded) { ++pending; continue; }
            uint64_t size = 0, modified = 0;
            if (sync_info(file->valuestring, &size, &modified)) { ++pending; continue; }
            FILETIME clock; GetSystemTimeAsFileTime(&clock);
            uint64_t now = ((uint64_t)clock.dwHighDateTime << 32) | clock.dwLowDateTime;
            /* Growing logs wait for the uploader's 60-second stability window.
             * Hash only after a matching-time receipt or a stable file. */
            if (sync_uploaded_at(capture_text(receipt, "uploadedAt")) < modified &&
                (modified > now || now - modified < UINT64_C(600000000))) { ++pending; continue; }
            const char *hash = sync_hash(cache, file->valuestring);
            if (hash && uploaded && !strcmp(hash, uploaded)) ++acknowledged; else ++pending;
        }
    }
    free(collector); free(path); cJSON_Delete(consent); cJSON_Delete(state); cJSON_Delete(cfg);
    return pending ? SF_SYNC_PENDING : acknowledged ? SF_SYNC_DONE : SF_SYNC_READY;
}
static int sync_state(SyncCache *cache, const char *dir) { return sync_state_mode(cache, dir, 0, 0); }
#endif
