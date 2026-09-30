#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

static const char guardKey;

void *teamCrossTrayIcon(int *length) {
    NSImage *image = [NSImage imageWithSystemSymbolName:@"person.2.fill" accessibilityDescription:@"Team Cross"];
    image.size = NSMakeSize(18, 18);
    NSBitmapImageRep *bitmap = [NSBitmapImageRep imageRepWithData:image.TIFFRepresentation];
    NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    if (!png.length || png.length > 65536) return NULL;
    void *bytes = malloc(png.length);
    if (!bytes) return NULL;
    memcpy(bytes, png.bytes, png.length);
    *length = (int)png.length;
    return bytes;
}

static BOOL internalURL(NSURL *url) {
    return [url.scheme isEqualToString:@"wails"] && [url.host isEqualToString:@"localhost"] && !url.user && !url.password && !url.port;
}
static WKWebView *findWebView(NSView *view) {
    if ([view isKindOfClass:WKWebView.class]) return (WKWebView *)view;
    for (NSView *child in view.subviews) { WKWebView *web = findWebView(child); if (web) return web; }
    return nil;
}

@interface TeamCrossWebGuard : NSObject <WKNavigationDelegate, WKUIDelegate>
@property (weak) id<WKNavigationDelegate> navigation;
@property (weak) id<WKUIDelegate> ui;
@property BOOL loaded;
@property NSMutableArray<NSString *> *pendingScripts;
@end

@implementation TeamCrossWebGuard
- (void)webView:(WKWebView *)webView didStartProvisionalNavigation:(WKNavigation *)navigation {
    self.loaded = NO;
    if ([self.navigation respondsToSelector:_cmd]) [self.navigation webView:webView didStartProvisionalNavigation:navigation];
}
- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)navigation {
    self.loaded = YES;
    for (NSString *script in self.pendingScripts) [webView evaluateJavaScript:script completionHandler:nil];
    [self.pendingScripts removeAllObjects];
    if ([self.navigation respondsToSelector:_cmd]) [self.navigation webView:webView didFinishNavigation:navigation];
}
- (BOOL)respondsToSelector:(SEL)selector {
    return [super respondsToSelector:selector] || [self.navigation respondsToSelector:selector] || [self.ui respondsToSelector:selector];
}
- (id)forwardingTargetForSelector:(SEL)selector {
    if ([self.navigation respondsToSelector:selector]) return self.navigation;
    if ([self.ui respondsToSelector:selector]) return self.ui;
    return [super forwardingTargetForSelector:selector];
}
- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action decisionHandler:(void (^)(WKNavigationActionPolicy))decision {
    NSURL *url = action.request.URL;
    BOOL page = internalURL(url) && ![url.path hasPrefix:@"/api/"] && ![url.path hasPrefix:@"/desktop/"];
    if (page && action.targetFrame.isMainFrame) { decision(WKNavigationActionPolicyAllow); return; }
    BOOL webLink = ([url.scheme isEqualToString:@"https"] || [url.scheme isEqualToString:@"http"]) && url.host.length && !url.user && !url.password;
    if (action.navigationType == WKNavigationTypeLinkActivated && action.sourceFrame.isMainFrame && internalURL(action.sourceFrame.request.URL) && webLink) {
        [NSWorkspace.sharedWorkspace openURL:url];
    }
    decision(WKNavigationActionPolicyCancel);
}
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features { return nil; }
- (void)webView:(WKWebView *)webView runJavaScriptConfirmPanelWithMessage:(NSString *)message initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(BOOL))completion {
    if (!frame.isMainFrame || !internalURL(frame.request.URL)) { completion(NO); return; }
    NSAlert *alert = [NSAlert new]; alert.messageText = @"Team Cross"; alert.informativeText = message;
    [alert addButtonWithTitle:@"确认 / Confirm"]; [alert addButtonWithTitle:@"取消 / Cancel"];
    [alert beginSheetModalForWindow:webView.window completionHandler:^(NSModalResponse response) { completion(response == NSAlertFirstButtonReturn); }];
}
@end

bool configureTeamCrossWindow(void *pointer) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    WKWebView *web = findWebView(window.contentView);
    if (!web) return false;
    if (objc_getAssociatedObject(web, &guardKey)) return true;
    TeamCrossWebGuard *guard = [TeamCrossWebGuard new];
    guard.pendingScripts = [NSMutableArray new];
    guard.loaded = !web.loading && internalURL(web.URL);
    guard.navigation = web.navigationDelegate; guard.ui = web.UIDelegate;
    objc_setAssociatedObject(web, &guardKey, guard, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    web.navigationDelegate = guard; web.UIDelegate = guard;
    return true;
}

void teamCrossWindowScript(void *pointer, const char *text) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    WKWebView *web = findWebView(window.contentView);
    TeamCrossWebGuard *guard = objc_getAssociatedObject(web, &guardKey);
    if (!guard || !text) return;
    NSString *script = [NSString stringWithUTF8String:text];
    if (guard.loaded) [web evaluateJavaScript:script completionHandler:nil];
    else if (guard.pendingScripts.count < 16) [guard.pendingScripts addObject:script];
}
