# Build the chapter 14 clox: chunks, constants, lines, and a disassembler.
#
#   make                 debug build at build/debug-libc/clox
#   make test            golden disassembly diff + the allocator unit tests
#   make test HEAP=own   the same, with reallocate() served by heap.c
#   make MODE=release    -O3 -flto
#   make asan            rebuild under AddressSanitizer + UBSan and test
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
else
	CFLAGS += -O0 -g -DDEBUG
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

SOURCES = main.c chunk.c debug.c heap.c line.c memory.c value.c
OBJECTS = $(addprefix $(BUILD_DIR)/, $(SOURCES:.c=.o))
HEADERS = $(wildcard *.h)

CLOX = $(BUILD_DIR)/clox
TEST_HEAP = $(BUILD_DIR)/test_heap

.PHONY: all test asan clean

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

test: $(CLOX) $(TEST_HEAP)
	@ $(TEST_HEAP)
	@ $(CLOX) > $(BUILD_DIR)/actual.txt
	@ diff -u test/expected.txt $(BUILD_DIR)/actual.txt \
		&& echo "golden: ok ($(MODE), heap=$(HEAP))"

asan:
	@ $(MAKE) test SAN=on

clean:
	@ rm -rf build
