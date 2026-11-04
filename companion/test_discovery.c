#define main sf_launcher_main
#define wmain sf_launcher_wmain
#include "launcher.c"
#undef main
#undef wmain
#ifndef _WIN32
#include "discovery.h"
#endif
#include <assert.h>
static void fixture(const char *game, const char *flavor, int exe) {
    char *path = malloc(PATH_CAP); assert(path); assert(!mkdir_tree((char *)game));
    assert(!addon_join(path, game, ".flavor.info")); assert(!capture_atomic(path, flavor, strlen(flavor)));
    if (exe) { assert(!addon_join(path, game, "Wow.exe")); assert(!capture_atomic(path, "fixture", 7)); }
    free(path);
}
static int cancelled(void *p) { (void)p; return 1; }
int main(int argc, char **argv) {
    assert(argc == 2);
    char (*p)[PATH_CAP] = calloc(5, sizeof(*p)); assert(p);
    assert(!addon_join(p[0], argv[1], "nested/other name/renamed client"));
    fixture(p[0], "Product Flavor!STRING:0\r\nwowf\r\n", 1);
    assert(addon_is_forever(p[0]));
    assert(!addon_join(p[1], argv[1], "_forever_"));
    fixture(p[1], "Product Flavor!STRING:0\nwow\n", 1);
    assert(!addon_is_forever(p[1]) && addon_install(p[1]) == -1);
    fixture(p[1], "Product Flavor!STRING:0\nwow_classic_beta\n", 1); assert(!addon_is_forever(p[1]));
    fixture(p[1], "Product Flavor!STRING:0\nwow_classic\n", 1); assert(!addon_is_forever(p[1]));
    fixture(p[1], "Product Flavor!STRING:0\nwowf\nextra\n", 1); assert(!addon_is_forever(p[1]));
    assert(!addon_join(p[2], argv[1], "no-executable"));
    fixture(p[2], "Product Flavor!STRING:0\nwowf\n", 0); assert(!addon_is_forever(p[2]));
    assert(discovery_tree(argv[1], p[3], NULL, NULL, NULL) == 1 && !strcmp(p[3], p[0]));
    assert(discovery_tree(argv[1], p[3], cancelled, NULL, NULL) == -1);
    assert(discovery_tree(p[2], p[3], NULL, NULL, NULL) == 0);
#ifndef _WIN32
    assert(!addon_join(p[4], p[2], "loop") && !symlink(argv[1], p[4]));
    assert(discovery_tree(p[2], p[3], NULL, NULL, NULL) == 0);
#endif
    free(p); puts("discovery checks passed: product identity, renamed clients, recursive selection, cancellation, no linked-directory loops"); return 0;
}
