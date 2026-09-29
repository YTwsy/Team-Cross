#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

static const char guardKey;
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
@end

@implementation TeamCrossWebGuard
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
    guard.navigation = web.navigationDelegate; guard.ui = web.UIDelegate;
    objc_setAssociatedObject(web, &guardKey, guard, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    web.navigationDelegate = guard; web.UIDelegate = guard;
    return true;
}
