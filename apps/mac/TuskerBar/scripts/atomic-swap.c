#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#ifndef RENAME_SWAP
#define RENAME_SWAP 0x00000002
#endif
#ifndef RENAME_EXCL
#define RENAME_EXCL 0x00000004
#endif

int main(int argc, char **argv) {
  if (argc != 3) {
    fprintf(stderr, "usage: atomic-swap SOURCE DESTINATION\n");
    return 64;
  }
  if (renameatx_np(AT_FDCWD, argv[1], AT_FDCWD, argv[2], RENAME_SWAP) == 0) {
    return 0;
  }
  if (errno == ENOENT &&
      renameatx_np(AT_FDCWD, argv[1], AT_FDCWD, argv[2], RENAME_EXCL) == 0) {
    return 0;
  }
  fprintf(stderr, "atomic swap failed: %s\n", strerror(errno));
  return 1;
}
