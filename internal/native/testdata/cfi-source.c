__attribute__((noinline)) int crash_frame(int x) { return x / 0; }
__attribute__((noinline)) int middle_frame(int x) { return crash_frame(x + 1); }
__attribute__((noinline)) int outer_frame(int x) { return middle_frame(x + 2); }
int main(void) { return outer_frame(1); }
