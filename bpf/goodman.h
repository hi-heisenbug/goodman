#ifndef GOODMAN_H
#define GOODMAN_H

#define TASK_COMM_LEN   16
#define MAX_STACK_DEPTH 32
#define PATH_MAX_LEN    256

enum event_type {
    EVENT_FILE_OPEN = 1,
    EVENT_NET_CONNECT = 2,
    EVENT_PROC_EXEC = 3,
    EVENT_DENY_FILE_OPEN = 4,
    EVENT_DENY_CONNECT = 5,
    EVENT_DENY_EXEC = 6,
};

/* Wire layout is shared with Go (internal/model/types.go RawEvent).
 * Fields are ordered so natural alignment leaves no implicit padding:
 * explicit _pad keeps the layout identical on both sides. */
struct event {
    __u32 pid;                       /* process id (tgid) */
    __u32 tid;                       /* thread id */
    __s32 dirfd;                     /* openat base fd; AT_FDCWD for open/exec */
    __u8  type;                      /* enum event_type */
    char  comm[TASK_COMM_LEN];       /* process name (e.g. "node") */
    char  arg[PATH_MAX_LEN];         /* file path, "ip:port", or exec path */
    __u8  _pad[3];                   /* align stack_len to 4 bytes */
    __u32 stack_len;                 /* number of valid entries in stack[] */
    __u8  _stack_pad[4];             /* align stack[] to 8 bytes */
    __u64 stack[MAX_STACK_DEPTH];    /* user-space instruction pointers */
    __u64 timestamp_ns;
};

#endif
