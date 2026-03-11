#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <time.h>
#include <unistd.h>

static volatile sig_atomic_t g_resized = 0;
static void on_sigwinch(int sig) { (void)sig; g_resized = 1; }

static void get_term_size(int *W, int *H) {
    struct winsize ws;
    if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) == 0 && ws.ws_col > 0 && ws.ws_row > 0) {
        *W = (int)ws.ws_col;
        *H = (int)ws.ws_row;
    } else {
        *W = 80; *H = 24;
    }
}

static const char *pal[] = {
    "\x1b[38;5;232m", "\x1b[38;5;52m",  "\x1b[38;5;88m",  "\x1b[38;5;124m",
    "\x1b[38;5;160m", "\x1b[38;5;196m", "\x1b[38;5;202m", "\x1b[38;5;208m",
    "\x1b[38;5;214m", "\x1b[38;5;220m", "\x1b[38;5;226m", "\x1b[38;5;231m",
};
#define NPAL 12

static const char chars[] = " .,;!*#$@";
#define NCHARS (int)(sizeof(chars) - 1)

static void init_bottom(unsigned char *g, int W, int H) {
    for (int x = 0; x < W; x++)
        g[(H - 1) * W + x] = 255;
}

static void spread(unsigned char *src, unsigned char *dst, int W, int H) {
    for (int y = 0; y < H - 1; y++) {
        for (int x = 0; x < W; x++) {
            int b = src[(y + 1) * W + x];
            int l = src[(y + 1) * W + (x > 0     ? x - 1 : x)];
            int r = src[(y + 1) * W + (x < W - 1 ? x + 1 : x)];
            int heat = (b + l + r) / 3 - (rand() % 3);
            dst[y * W + x] = (unsigned char)(heat < 0 ? 0 : heat);
        }
    }
    for (int x = 0; x < W; x++) {
        int h = 255 - (rand() % 30);
        dst[(H - 1) * W + x] = (unsigned char)(h < 0 ? 0 : h);
    }
}

int main(void) {
    signal(SIGWINCH, on_sigwinch);
    srand((unsigned)time(NULL));

    int W, H;
    get_term_size(&W, &H);

    unsigned char *a = calloc(W * H, 1);
    unsigned char *b = calloc(W * H, 1);
    if (!a || !b) return 1;

    init_bottom(a, W, H);
    printf("\x1b[2J\x1b[?25l");

    for (;;) {
        if (g_resized) {
            g_resized = 0;
            int nW, nH;
            get_term_size(&nW, &nH);
            if (nW != W || nH != H) {
                unsigned char *na = realloc(a, nW * nH);
                unsigned char *nb = realloc(b, nW * nH);
                if (na && nb) {
                    a = na; b = nb;
                    memset(a, 0, nW * nH);
                    W = nW; H = nH;
                    init_bottom(a, W, H);
                }
            }
        }

        spread(a, b, W, H);
        unsigned char *tmp = a; a = b; b = tmp;

        printf("\x1b[H");
        for (int y = 0; y < H; y++) {
            for (int x = 0; x < W; x++) {
                int heat = a[y * W + x];
                fputs(pal[heat * (NPAL - 1) / 255], stdout);
                fputc(chars[heat * (NCHARS - 1) / 255], stdout);
            }
            if (y < H - 1) putchar('\n');
        }
        printf("\x1b[0m");
        fflush(stdout);
        usleep(50000);
    }

    free(a); free(b);
    return 0;
}
