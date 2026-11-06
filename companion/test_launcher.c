#define main launcher_main
#define wmain launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
#include <assert.h>

static const char *manifest;
static const char *binary;
static int corrupt, offline;

static int fake_fetch(const char *url, const char *out, size_t limit) {
    if (offline) return -1;
    const char *src = strstr(url, "latest.txt") ? manifest : binary;
    FILE *in = open_file(src, "rb");
    FILE *f = new_file(out);
    uint64_t n;
    assert(in && f && !file_size(in, &n));
    if (n > limit) { fclose(in); fclose(f); remove_file(out); return -1; }
    assert(!copy_bytes(in, f, n));
    fclose(in); fclose(f);
    if (corrupt && !strstr(url, "latest.txt")) {
        f = open_file(out, "r+b"); assert(f); fputc('!', f); fclose(f);
    }
    return 0;
}
static void bytes(const char *path, const char *value) {
    FILE *f = open_file(path, "wb"); assert(f);
    assert(fwrite(value, 1, strlen(value), f) == strlen(value)); assert(!fclose(f));
}
static Release release_for(const char *path, const char *version) {
    Release r = {0}; unsigned char d[32]; FILE *f = open_file(path, "rb"); assert(f);
    assert(!file_size(f, &r.size) && !sha256(f, r.size, d)); fclose(f);
    hex_digest(d, r.hash); strcpy(r.version, version); return r;
}
static void restart_checks(void) {
    /* A restart keeps the player's arguments and this run's switches. */
    char *in[] = {"old", "-config", "a \"quoted\" dir", "-logs=false"};
    no_collect = 1; no_logs = 1;
    char **r = restart_args("new", 4, in); assert(r);
    assert(!strcmp(r[0], "new") && !strcmp(r[1], "-config") && !strcmp(r[2], "a \"quoted\" dir") && !strcmp(r[3], "-logs=false"));
    assert(!strcmp(r[4], "--from-update") && !strcmp(r[5], "--no-collect") && !strcmp(r[6], "--no-logs"));
#ifdef _WIN32
    assert(!strcmp(r[7], "--background") && !r[8]);
#else
    assert(!r[7]);
#endif
    free(r); no_collect = 0; no_logs = 0;
    assert(SF_UPDATE_EVERY_SECONDS >= 60);
}
int main(int argc, char **argv) {
    assert(argc == 2); restart_checks();
    char *base = argv[1]; assert(!mkdir_tree(base));
    char payload[PATH_CAP], raw[PATH_CAP], bundled[PATH_CAP], extracted[PATH_CAP], next[PATH_CAP], mf[PATH_CAP];
    assert(!path_join(payload, base, "payload") && !path_join(raw, base, "raw") && !path_join(bundled, base, "bundle") && !path_join(mf, base, "manifest"));
    bytes(payload, "abc"); bytes(raw, "launcher");
    unsigned current[3]; assert(!version_parts(SF_VERSION, current)); assert(current[0] < 999999);
    char newer[32]; snprintf(newer, sizeof(newer), "%u.0.0", current[0] + 1);
    Release r = release_for(payload, newer);
    assert(!strcmp(r.hash, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"));
    assert(!verify(payload, &r)); r.size++; assert(verify(payload, &r)); r.size--;
    assert(compare_version("0.33.0", "0.32.0") == 1);
    assert(compare_version("0.9.9", "0.10.0") == -1);
    assert(compare_version("1.0.0", "1.0.0") == 0);
    const char *bad[] = {"", "01.0.0", "1.2", "1.2.3.4", "1.2.-1", "1.2.3-test", "1.2.999999999999", "../1.2.3"};
    for (size_t i = 0; i < sizeof(bad) / sizeof(*bad); ++i) assert(compare_version(bad[i], "1.0.0") == INT_MIN);
    assert(!package(raw, payload, bundled)); assert(!extract(bundled, base, extracted)); assert(!verify(extracted, &r));
    assert(!extract(bundled, base, extracted));
    bytes(extracted, "broken"); assert(!extract(bundled, base, extracted)); assert(!verify(extracted, &r));
    bytes(bundled, "truncated"); assert(extract(bundled, base, extracted));
    assert(!write_release(mf, &r));
    FILE *f = open_file(mf, "rb"); Release parsed; assert(!read_release(f, &parsed)); fclose(f);
    assert(!strcmp(parsed.version, r.version) && parsed.size == r.size);
    manifest = mf; binary = payload;
    corrupt = 1; assert(update(base, next, fake_fetch) < 0);
    char pointer[PATH_CAP]; assert(!pointer_path(pointer, base)); assert(!open_file(pointer, "rb"));
    corrupt = 0; assert(update(base, next, fake_fetch) == 1); assert(!verify(next, &r));
    offline = 1; assert(update(base, next, fake_fetch) == 1); assert(!verify(next, &r)); offline = 0;
    remove_file(mf); Release old = r; strcpy(old.version, "0.31.0"); assert(!write_release(mf, &old));
    assert(update(base, next, fake_fetch) == 1); assert(!verify(next, &r));
    bytes(mf, "SIMFOREVER-COMPANION-1\n../../evil\n3\nabc\n");
    assert(update(base, next, fake_fetch) == 1);
    remove_file(pointer);
    assert(update(base, next, fake_fetch) < 0);
    const char *invalid[] = {"SIMFOREVER-COMPANION-1\n1.2.3\n0\n", "SIMFOREVER-COMPANION-1\n1.2.3\n999999999999999999999\n", "bad\n1.2.3\n3\nhash\n", "SIMFOREVER-COMPANION-1\n1.2.3\n3\nabc\nextra\n"};
    for (size_t i = 0; i < sizeof(invalid) / sizeof(*invalid); ++i) {
        bytes(mf, invalid[i]); f = open_file(mf, "rb"); assert(read_release(f, &parsed)); fclose(f);
    }
    bytes(payload, "corrupt cache");
    assert(verify(payload, &r));
    assert(!write_release(pointer, &r)); bytes(next, "corrupt cached release");
    offline = 1; assert(update(base, next, fake_fetch) < 0);
    char denied[PATH_CAP]; assert(!path_join(denied, base, "denied"));
    assert(download("http://127.0.0.1/not-allowed", denied, 255));
    assert(!open_file(denied, "rb"));
    puts("ok: hashes, packaging, extraction, releases, corruption, offline fallback, downgrade refusal, HTTPS requirement");
    return 0;
}
