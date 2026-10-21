# Native parser fixtures

elf-dwarf-amd64 and its source are original GoodSentry fixtures compiled with:

    gcc -g -O0 -no-pie -fno-asynchronous-unwind-tables -o elf-dwarf-amd64 elf-dwarf-source.c

macho-dsym-amd64 is the decoded
debug/macho/testdata/gcc-amd64-darwin-exec-debug.base64 fixture from Go
1.27.2. It is a Mach-O dSYM companion file whose file-backed __DWARF
segment has a different address from its zero-byte __TEXT segment. It
ensures image-relative lookup uses the executable base instead of the
debug-data segment. The Go license is included as go-LICENSE.
