#define main sf_launcher_main
#define wmain sf_launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
int main(int argc, char **argv) {
    const char *config = NULL;
    for (int i = 1; i + 1 < argc; ++i) if (!strcmp(argv[i], "-config")) config = argv[++i];
    if (!config) return 1;
    char *path = malloc(PATH_CAP); if (!path || addon_join(path, config, "heartbeat")) return 1;
    size_t previous_size = 0; char *previous = capture_read(path, &previous_size);
    unsigned first = previous ? (unsigned)strtoul(previous, NULL, 10) + 1 : 1; free(previous);
    /* Keep the heartbeat monotonic across pause/resume worker restarts. */
    char *stop = malloc(PATH_CAP); if (!stop || addon_join(stop, config, "stop")) return 1;
    for (unsigned count = first;; ++count) {
        if (addon_kind(stop) == 1) return 0; /* the restart check's end */
        char number[32]; int n = snprintf(number, sizeof(number), "%u", count);
        if (capture_atomic(path, number, (size_t)n)) return 1;
        Sleep(100);
    }
}
