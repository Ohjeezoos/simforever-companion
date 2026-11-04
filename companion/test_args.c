#include <stdio.h>
#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdlib.h>
int wmain(int argc, wchar_t **argv) {
    for (int i = 1; i < argc; ++i) {
        int n = WideCharToMultiByte(CP_UTF8, 0, argv[i], -1, NULL, 0, NULL, NULL);
        char *s = n ? malloc((size_t)n) : NULL;
        if (!s || !WideCharToMultiByte(CP_UTF8, 0, argv[i], -1, s, n, NULL, NULL)) return 1;
        for (unsigned char *p = (unsigned char *)s; *p; ++p) printf("%02x", *p);
        putchar('\n'); free(s);
    }
    return 0;
}
#else
int main(int argc, char **argv) {
    for (int i = 1; i < argc; ++i) {
        for (unsigned char *p = (unsigned char *)argv[i]; *p; ++p) printf("%02x", *p);
        putchar('\n');
    }
    return 0;
}
#endif
