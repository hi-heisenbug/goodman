#ifndef GOODMAN_H
#define GOODMAN_H

#define TASK_COMM_LEN   16
#define MAX_STACK_DEPTH 32
#define PATH_MAX_LEN    256

enum event_type {
    EVENT_FILE_OPEN = 1,
    EVENT_NET_CONNECT = 2,
    EVENT_PROC_EXEC = 3,
};

/* Wire layout is shared with Go (internal/model/types.go RawEvent).
 * Fields are ordered so natural alignment leaves no implicit padding:
 * explicit _pad keeps the layout identical on both sides. */
struct event {
    __u32 pid;                       /* process id (tgid) */
    __u32 tid;                       /* thread id */
    __u8  type;                      /* enum event_type */
    char  comm[TASK_COMM_LEN];       /* process name (e.g. "node") */
    char  arg[PATH_MAX_LEN];         /* file path, "ip:port", or exec path */
    __u8  _pad[3];                   /* align stack[] to 8 bytes (4+4+1+16+256+3 = 284 -> pad to 288) */
    __u32 stack_len;                 /* number of valid entries in stack[] */
    __u64 stack[MAX_STACK_DEPTH];    /* user-space instruction pointers */
    __u64 timestamp_ns;
};

#endif
