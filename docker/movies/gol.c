#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <signal.h>
#include <sys/ioctl.h>

static volatile sig_atomic_t g_resized = 0;
static void on_sigwinch(int sig) { (void)sig; g_resized = 1; }

static void get_term_size(int *W, int *H) {
    struct winsize ws;
    if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) == 0 && ws.ws_col > 0 && ws.ws_row > 0) {
        *W = (int)ws.ws_col;
        *H = (int)ws.ws_row;
    } else {
        *W = 80;
        *H = 24;
    }
}

/* ~17% density — sparse enough for gliders and oscillators to emerge */
static void seed(unsigned char *g, int W, int GH) {
    memset(g, 0, (size_t)(W * GH));
    for (int i = 0; i < W * GH; i++)
        g[i] = (rand() % 6 == 0) ? 1 : 0;
}

static int neighbors(const unsigned char *g, int W, int GH, int x, int y) {
    int c = 0;
    for (int dy = -1; dy <= 1; dy++)
        for (int dx = -1; dx <= 1; dx++) {
            if (!dx && !dy) continue;
            c += g[((y + dy + GH) % GH) * W + (x + dx + W) % W];
        }
    return c;
}

int main(void) {
    srand((unsigned)time(NULL));
    signal(SIGWINCH, on_sigwinch);

    int W, H;
    get_term_size(&W, &H);
    int GH = H * 2;  /* grid is twice the terminal height; each char row = 2 cells */

    unsigned char *cur  = malloc((size_t)(W * GH));
    unsigned char *next = malloc((size_t)(W * GH));
    char          *line = malloc((size_t)(W * 3 + 2));

    fputs("\x1b[?25l\x1b[2J", stdout);

    seed(cur, W, GH);
    int prev_pop = -1, stale = 0;

    for (;;) {
        if (g_resized) {
            g_resized = 0;
            free(cur); free(next); free(line);
            get_term_size(&W, &H);
            GH = H * 2;
            cur  = malloc((size_t)(W * GH));
            next = malloc((size_t)(W * GH));
            line = malloc((size_t)(W * 3 + 2));
            fputs("\x1b[2J", stdout);
            seed(cur, W, GH);
            prev_pop = -1; stale = 0;
        }

        fputs("\x1b[H", stdout);
        int pop = 0;
        for (int y = 0; y < H; y++) {
            int p = 0;
            for (int x = 0; x < W; x++) {
                int top = cur[(2 * y)     * W + x];
                int bot = cur[(2 * y + 1) * W + x];
                pop += top + bot;
                /* half-block characters pack two grid rows into one terminal row */
                if (!top && !bot) {
                    line[p++] = ' ';
                } else if (top && !bot) {
                    line[p++] = (char)0xe2; line[p++] = (char)0x96; line[p++] = (char)0x80; /* ▀ */
                } else if (!top && bot) {
                    line[p++] = (char)0xe2; line[p++] = (char)0x96; line[p++] = (char)0x84; /* ▄ */
                } else {
                    line[p++] = (char)0xe2; line[p++] = (char)0x96; line[p++] = (char)0x88; /* █ */
                }
            }
            if (y < H - 1) line[p++] = '\n';  /* skip on last row to prevent scroll */
            fwrite(line, 1, (size_t)p, stdout);
        }
        fflush(stdout);

        if (pop == prev_pop) stale++;
        else stale = 0;
        prev_pop = pop;

        if (pop < 5 || stale > 40) {
            usleep(600000);
            fputs("\x1b[2J", stdout);
            seed(cur, W, GH);
            prev_pop = -1; stale = 0;
            continue;
        }

        for (int y = 0; y < GH; y++)
            for (int x = 0; x < W; x++) {
                int n = neighbors(cur, W, GH, x, y);
                next[y * W + x] = cur[y * W + x] ? (n == 2 || n == 3) : (n == 3);
            }

        unsigned char *tmp = cur; cur = next; next = tmp;
        usleep(80000);
    }
}
