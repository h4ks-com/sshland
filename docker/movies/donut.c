#include <math.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <unistd.h>

static volatile sig_atomic_t g_resized = 0;
static void on_sigwinch(int sig) { (void)sig; g_resized = 1; }

static void get_term_size(int *W, int *H, float *ar) {
    struct winsize ws;
    if (ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws) == 0 && ws.ws_col > 0 && ws.ws_row > 0) {
        *W = (int)ws.ws_col;
        *H = (int)ws.ws_row;
        /* Compute char aspect ratio from pixel dims when available.
           Some SSH stacks fill pixel dims as cols×8/rows×8 (implying 1:1 ratio);
           reject values outside a plausible range and fall back to 2.0. */
        if (ws.ws_xpixel > 0 && ws.ws_ypixel > 0) {
            float computed = ((float)ws.ws_ypixel / ws.ws_row) / ((float)ws.ws_xpixel / ws.ws_col);
            *ar = (computed >= 1.2f && computed <= 4.0f) ? computed : 2.0f;
        } else {
            *ar = 2.0f;
        }
    } else {
        *W = 80; *H = 22; *ar = 2.0f;
    }
}

int main(void) {
    signal(SIGWINCH, on_sigwinch);
    float A = 0.0f, B = 0.0f;
    printf("\x1b[2J");

    for (;;) {
        g_resized = 0;
        int W, H;
        float ar;
        get_term_size(&W, &H, &ar);

        int sz = W * H;
        float *z = malloc(sz * sizeof(float));
        char  *b = malloc(sz);
        if (!z || !b) { free(z); free(b); usleep(30000); continue; }

        memset(b, ' ', sz);
        memset(z, 0, sz * sizeof(float));

        float xo = W * 0.5f, yo = H * 0.5f;
        /* Anchor to terminal height so the torus never clips vertically.
           xp follows from the char aspect ratio so columns and rows map to
           equal physical distances — making the torus appear circular. */
        float yp = (float)H * 0.625f;
        float xp = yp * ar;
        if (xp > (float)W * 0.9f) { xp = (float)W * 0.9f; yp = xp / ar; }

        float jstep = fmaxf(0.01f,  2.1f / xp);
        float istep = fmaxf(0.002f, 0.6f  / xp);

        for (float j = 0; j < 6.28f; j += jstep) {
            for (float i = 0; i < 6.28f; i += istep) {
                float c = sinf(i), d = cosf(j), e = sinf(A),
                      f = sinf(j), g = cosf(A), h = d + 2,
                      D = 1.0f / (c * h * e + f * g + 5),
                      l = cosf(i), m = cosf(B), n = sinf(B),
                      t = c * h * g - f * e;
                int x = (int)(xo + xp * D * (l * m - t * n));
                int y = (int)(yo + yp * D * (l * n + t * m));
                if (y >= 0 && y < H && x >= 0 && x < W) {
                    int o = x + W * y;
                    if (D > z[o]) {
                        int N = (int)(8 * ((f * e - c * d * g) * m - c * d * e - f * g - l * d * n));
                        if (N < 0) N = 0;
                        if (N > 11) N = 11;
                        z[o] = D;
                        b[o] = ".,-~:;=!*#$@"[N];
                    }
                }
            }
        }

        printf("\x1b[H");
        for (int k = 0; k < sz; k++) {
            if (k > 0 && k % W == 0) putchar('\n');
            putchar(b[k]);
        }
        fflush(stdout);

        free(z); free(b);
        A += 0.04f; B += 0.02f;
        usleep(30000);
    }
}
