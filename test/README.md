# Vendored: the Crafting Interpreters test suite

265 `.lox` files copied verbatim from
[munificent/craftinginterpreters](https://github.com/munificent/craftinginterpreters),
commit `4a840f70f69c6ddd17cfef4f6964f8e1bcd8c3d4` (2024-08-01). Nothing here is
generated and nothing here is edited — if a test looks wrong, it is wrong
upstream, and the fix is a patch there rather than a local change.

Run them with:

    tool/booktest.sh

which builds the interpreter and runs this directory against it on every parser
configuration. `tool/booktest.py` is the runner and holds the details: which
tests the book's own jlox suite skips and why, how expectation comments are
spelled, and the two tests we fail on purpose because this interpreter
implements chapter 12's class-methods challenge and jlox does not.

`tool/booktest.sh -u` ignores this directory, clones upstream fresh into
`.booktest/` and runs that instead, which is how to tell whether this copy has
fallen behind.

## Re-vendoring

    git clone --depth 1 https://github.com/munificent/craftinginterpreters.git /tmp/ci
    rm -rf test && cp -R /tmp/ci/test test
    git -C /tmp/ci rev-parse HEAD     # update the commit above

Then restore `LICENSE` and this file, which are ours and not upstream's.

## Why vendored rather than cloned

The suite is the acceptance test for this interpreter, and an acceptance test
that only runs when the network is up is one that stops running. Pinning it also
means a change in the results is a change in *this* repository — upstream cannot
move under us and turn a green run red, or a red run green.

The cost is ~1.2 MB in the tree and a copy that goes stale silently. `-u` is the
answer to the second one.
