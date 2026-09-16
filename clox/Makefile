# Build clox through chapter 16: chunks, a disassembler, a virtual machine, and
# a scanner behind a REPL.
#
#   make                 debug build at build/debug-libc/clox
#   make test            three golden diffs, plus the allocator unit tests
#   make test HEAP=own   the same, with reallocate() served by heap.c
#   make MODE=release    -O3 -flto
#   make asan            rebuild under AddressSanitizer + UBSan and test
#   make bench           time OP_NEGATE both ways at -O3 (challenge 4)
#   make clean
#
# The flags are the book's own, from util/c.make in the craftinginterpreters
# repo, so a warning here is a warning there.

MODE ?= debug
HEAP ?= libc

CC = cc
CFLAGS = -std=c99 -Wall -Wextra -Werror -Wno-unused-parameter

ifeq ($(MODE),release)
	CFLAGS += -O3 -flto
	TRACE_CHECK = test ! -s $(BUILD_DIR)/actual-trace.txt || \
		{ echo "release build wrote to stderr:"; \
		  cat $(BUILD_DIR)/actual-trace.txt; exit 1; }
else
	CFLAGS += -O0 -g -DDEBUG
	TRACE_CHECK = diff -u test/expected-trace.txt $(BUILD_DIR)/actual-trace.txt
endif

# CLOX_OWN_HEAP swaps the body of reallocate() over to heap.c. Nothing else in
# the program changes, which is the claim `make test HEAP=own` exists to check:
# the golden output must come out byte for byte identical either way.
ifeq ($(HEAP),own)
	CFLAGS += -DCLOX_OWN_HEAP
endif

BUILD_DIR = build/$(MODE)-$(HEAP)

# Sanitizers get their own build directory so the objects are never mixed with
# an ordinary build's.
ifeq ($(SAN),on)
	CFLAGS += -fsanitize=address,undefined -fno-omit-frame-pointer
	BUILD_DIR = build/$(MODE)-$(HEAP)-asan
endif

SOURCES = main.c chunk.c compiler.c debug.c heap.c line.c memory.c \
          scanner.c value.c vm.c
OBJECTS = $(addprefix $(BUILD_DIR)/, $(SOURCES:.c=.o))
HEADERS = $(wildcard *.h)

CLOX = $(BUILD_DIR)/clox
TEST_HEAP = $(BUILD_DIR)/test_heap
TEST_CHUNK = $(BUILD_DIR)/test_chunk

# Everything but main.o, so a test binary can supply its own entry point.
LIB_OBJECTS = $(filter-out $(BUILD_DIR)/main.o, $(OBJECTS))

.PHONY: all test asan bench clean

all: $(CLOX)

$(CLOX): $(OBJECTS)
	@ $(CC) $(CFLAGS) $(OBJECTS) -o $@

$(BUILD_DIR)/%.o: %.c $(HEADERS)
	@ mkdir -p $(BUILD_DIR)
	@ $(CC) $(CFLAGS) -c $< -o $@

# test_heap exercises heap.c directly rather than through reallocate(), so it
# builds the same object regardless of HEAP -- the allocator has to be correct
# before it is worth switching the interpreter onto it.
$(TEST_HEAP): test/test_heap.c $(BUILD_DIR)/heap.o $(HEADERS)
	@ mkdir -p $(BUILD_DIR)
	@ $(CC) $(CFLAGS) -I. test/test_heap.c $(BUILD_DIR)/heap.o -o $@

# test_chunk is the hand-built bytecode demo that used to be main(). Chapter 16
# gave main.c to the REPL, and nothing else can run bytecode until chapter 17.
$(TEST_CHUNK): test/test_chunk.c $(LIB_OBJECTS) $(HEADERS)
	@ mkdir -p $(BUILD_DIR)
	@ $(CC) $(CFLAGS) -I. test/test_chunk.c $(LIB_OBJECTS) -o $@

# Three golden diffs and the allocator's unit tests.
#
# test_chunk's stdout is the bytecode demo and must be identical in every
# configuration. Its stderr is the execution trace, which only a debug build
# emits -- so a release build is checked for saying nothing at all, which is the
# stronger claim. And clox itself scans test/tokens.lox, which is the first time
# the interpreter has been handed a .lox file.
test: $(CLOX) $(TEST_HEAP) $(TEST_CHUNK)
	@ $(TEST_HEAP)
	@ $(TEST_CHUNK) > $(BUILD_DIR)/actual.txt 2> $(BUILD_DIR)/actual-trace.txt
	@ diff -u test/expected.txt $(BUILD_DIR)/actual.txt
	@ $(TRACE_CHECK)
	@ $(CLOX) test/tokens.lox > $(BUILD_DIR)/actual-tokens.txt
	@ diff -u test/expected-tokens.txt $(BUILD_DIR)/actual-tokens.txt
	@ echo "golden: ok ($(MODE), heap=$(HEAP))"

asan:
	@ $(MAKE) test SAN=on

# bench answers challenge 4 with a number rather than an argument. It builds the
# VM twice at -O3 -- once negating in place, once with the book's push(-pop()) --
# and times the same chunk under each. No -DDEBUG, so no trace is compiled in;
# with one the measurement would be of fprintf.
#
# The benchmark's chunk ends in OP_RETURN, which prints. stdout goes to /dev/null
# and the timings come back on stderr.
BENCH_SOURCES = $(filter-out main.c, $(SOURCES)) test/bench_vm.c
BENCH_CFLAGS = -std=c99 -Wall -Wextra -Werror -Wno-unused-parameter -O3 -I.

bench:
	@ mkdir -p build/bench
	@ $(CC) $(BENCH_CFLAGS) $(BENCH_SOURCES) -o build/bench/inplace
	@ $(CC) $(BENCH_CFLAGS) -DCLOX_NEGATE_VIA_STACK $(BENCH_SOURCES) \
		-o build/bench/viastack
	@ ./build/bench/inplace  > /dev/null
	@ ./build/bench/viastack > /dev/null

clean:
	@ rm -rf build
