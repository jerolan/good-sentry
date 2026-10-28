# Native parser fixtures

elf-dwarf-amd64 and its source are original GoodSentry fixtures compiled with:

    gcc -g -O0 -no-pie -fno-asynchronous-unwind-tables -o elf-dwarf-amd64 elf-dwarf-source.c

cfi-amd64 and cfi-source.c are original GoodSentry fixtures compiled with:

    gcc -g -O1 -fomit-frame-pointer -no-pie -o cfi-amd64 cfi-source.c
    gcc -g -O1 -fomit-frame-pointer -fPIE -pie -o cfi-amd64-pie cfi-source.c

They exercise .eh_frame unwinding when RBP is zero and no function has a
frame-pointer prologue.

macho-dsym-amd64 is the decoded
debug/macho/testdata/gcc-amd64-darwin-exec-debug.base64 fixture from Go
1.27.2. It is a Mach-O dSYM companion file whose file-backed __DWARF
segment has a different address from its zero-byte __TEXT segment. It
ensures image-relative lookup uses the executable base instead of the
debug-data segment. The Go license is included as go-LICENSE.

portable-dotnet10.pdb is an original GoodSentry fixture built from
portable-dotnet10.cs in Debug configuration with .NET SDK 10.0.402. It contains
method sequence points with document switches and a hidden point. Its metadata
was independently inspected with System.Reflection.Metadata and PEReader;
the expected rows and offsets are recorded in portable-dotnet10-oracle.json.
The compiled sample program is not required to run the Go tests.
