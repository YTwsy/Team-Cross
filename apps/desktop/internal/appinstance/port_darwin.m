#include <CoreFoundation/CoreFoundation.h>
#include <dispatch/dispatch.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#import <Foundation/Foundation.h>

char *tcIdentityPath(char *directory) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:directory] isDirectory:YES];
        NSString *path = url.URLByStandardizingPath.URLByResolvingSymlinksInPath.path;
        return path ? strdup(path.UTF8String) : NULL;
    }
}

extern char *tcReceive(uintptr_t identity, char *data, int length);

static CFDataRef receive(CFMessagePortRef port, SInt32 message, CFDataRef data, void *info) {
    if (message != 1 || !data || CFDataGetLength(data) > (1 << 20)) return NULL;
    char *reply = tcReceive((uintptr_t)info, (char *)CFDataGetBytePtr(data), (int)CFDataGetLength(data));
    if (!reply) return NULL;
    CFDataRef result = CFDataCreate(NULL, (const UInt8 *)reply, strlen(reply));
    free(reply);
    return result;
}

void *tcPortCreate(char *name, uintptr_t identity) {
    CFStringRef key = CFStringCreateWithCString(NULL, name, kCFStringEncodingUTF8);
    CFMessagePortContext context = {0, (void *)identity, NULL, NULL, NULL};
    CFMessagePortRef port = CFMessagePortCreateLocal(NULL, key, receive, &context, NULL);
    CFRelease(key);
    if (!port) return NULL;
    // Receipts must not depend on an AppKit modal dialog finishing. The Go
    // callback only validates and enqueues; UI work happens independently.
    CFMessagePortSetDispatchQueue(port, dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0));
    return (void *)port;
}

void tcPortClose(void *pointer) {
    CFMessagePortRef port = (CFMessagePortRef)pointer;
    CFMessagePortInvalidate(port);
    CFRelease(port);
}

char *tcPortSend(char *name, char *data, int length) {
    CFStringRef key = CFStringCreateWithCString(NULL, name, kCFStringEncodingUTF8);
    CFMessagePortRef port = CFMessagePortCreateRemote(NULL, key);
    CFRelease(key);
    if (!port) return NULL;
    CFDataRef body = CFDataCreate(NULL, (const UInt8 *)data, length), reply = NULL;
    SInt32 status = CFMessagePortSendRequest(port, 1, body, 1, 1, kCFRunLoopDefaultMode, &reply);
    CFRelease(body);
    CFRelease(port);
    char *result = NULL;
    if (status == kCFMessagePortSuccess && reply && CFDataGetLength(reply) <= 4096) {
        CFIndex size = CFDataGetLength(reply);
        result = malloc(size + 1);
        if (result) { memcpy(result, CFDataGetBytePtr(reply), size); result[size] = 0; }
    }
    if (reply) CFRelease(reply);
    return result;
}
