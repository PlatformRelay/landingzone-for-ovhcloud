//go:build linux && (386 || amd64 || arm || arm64 || loong64 || riscv64 || s390x)

package main

// tcflsh is TCFLSH, the ioctl behind tcflush(3), as the generic Linux numbering these
// architectures share has it (include/uapi/asm-generic/ioctls.h; golang.org/x/sys/unix's
// zerrors_linux_<arch>.go agree). The syscall package does not export it. Other Linux
// architectures number it differently (mips 0x5407, ppc64 0x2000741f) and do not build lz-live,
// rather than issue another ioctl.
const tcflsh = 0x540B
