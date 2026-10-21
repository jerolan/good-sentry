/* Built with: gcc -g -O0 -no-pie -fno-asynchronous-unwind-tables -o elf-dwarf-amd64 elf-dwarf-source.c */
int fixture_function(int value) {
    return value + 1;
}

int main(void) {
    return fixture_function(0);
}
