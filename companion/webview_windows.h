/* C host for the same local HTML interface as the original companion. */
#ifndef SF_WEBVIEW_WINDOWS_H
#define SF_WEBVIEW_WINDOWS_H
#include "vendor/webview2/WebView2.h"
#include "ui_payload.h"

typedef struct SfView {
    LONG refs; int closing, ready;
    HWND window;
    wchar_t *page_uri;
    void *context;
    void (*message)(void *, const char *);
    ICoreWebView2Controller *controller;
    ICoreWebView2 *core;
    EventRegistrationToken web_token, nav_token, popup_token;
} SfView;
static void sf_view_ref(SfView *v) { InterlockedIncrement(&v->refs); }
static void sf_view_unref(SfView *v) { if (!InterlockedDecrement(&v->refs)) { free(v->page_uri); free(v); } }
static void sf_view_bounds(SfView *v) {
    if (v && v->controller) { RECT bounds; GetClientRect(v->window, &bounds); ICoreWebView2Controller_put_Bounds(v->controller, bounds); }
}
static void sf_view_visible(SfView *v, int visible) {
    if (v && v->controller) ICoreWebView2Controller_put_IsVisible(v->controller, visible);
}
static int sf_view_source(SfView *v, const wchar_t *source) {
    return source && (!wcscmp(source, L"about:blank") || (v->page_uri && !wcscmp(source, v->page_uri)));
}
static int sf_view_post(SfView *v, const char *json) {
    if (!v || !v->ready || !v->core || v->closing) return -1;
    wchar_t *w = wide(json); HRESULT rc = w ? ICoreWebView2_PostWebMessageAsJson(v->core, w) : E_OUTOFMEMORY;
    free(w); return SUCCEEDED(rc) ? 0 : -1;
}
#define SF_HANDLER(Name, Interface) \
    typedef struct { Interface iface; LONG refs; SfView *view; } Name; \
    static ULONG STDMETHODCALLTYPE Name##_add(Interface *self) { return (ULONG)InterlockedIncrement(&((Name *)self)->refs); } \
    static ULONG STDMETHODCALLTYPE Name##_release(Interface *self) { \
        Name *h = (Name *)self; LONG count = InterlockedDecrement(&h->refs); \
        if (!count) { sf_view_unref(h->view); free(h); } return (ULONG)count; \
    } \
    static HRESULT STDMETHODCALLTYPE Name##_query(Interface *self, REFIID iid, void **out) { \
        if (!out) return E_POINTER; *out = NULL; \
        if (!IsEqualIID(iid, &IID_IUnknown) && !IsEqualIID(iid, &IID_##Interface)) return E_NOINTERFACE; \
        *out = self; Name##_add(self); return S_OK; \
    }
SF_HANDLER(SfEnvironmentHandler, ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler)
SF_HANDLER(SfControllerHandler, ICoreWebView2CreateCoreWebView2ControllerCompletedHandler)
SF_HANDLER(SfMessageHandler, ICoreWebView2WebMessageReceivedEventHandler)
SF_HANDLER(SfNavigationHandler, ICoreWebView2NavigationStartingEventHandler)
SF_HANDLER(SfPopupHandler, ICoreWebView2NewWindowRequestedEventHandler)
#undef SF_HANDLER
#define SF_NEW_HANDLER(Type, vtable, owner) \
    Type *handler = calloc(1, sizeof(*handler)); \
    if (!handler) return E_OUTOFMEMORY; \
    handler->iface.lpVtbl = &vtable; handler->refs = 1; handler->view = owner; sf_view_ref(owner)

static HRESULT STDMETHODCALLTYPE sf_message(ICoreWebView2WebMessageReceivedEventHandler *self,
                                            ICoreWebView2 *sender, ICoreWebView2WebMessageReceivedEventArgs *args) {
    (void)sender; SfView *v = ((SfMessageHandler *)self)->view; LPWSTR source = NULL, message = NULL;
    if (v->closing) return S_OK;
    if (SUCCEEDED(ICoreWebView2WebMessageReceivedEventArgs_get_Source(args, &source)) && sf_view_source(v, source) &&
        SUCCEEDED(ICoreWebView2WebMessageReceivedEventArgs_TryGetWebMessageAsString(args, &message)) && message) {
        int n = WideCharToMultiByte(CP_UTF8, 0, message, -1, NULL, 0, NULL, NULL);
        char *utf = n > 0 && n < 4096 ? malloc((size_t)n) : NULL;
        if (utf && WideCharToMultiByte(CP_UTF8, 0, message, -1, utf, n, NULL, NULL)) {
            if (!strcmp(utf, "ready")) v->ready = 1;
            if (v->message) v->message(v->context, utf);
        }
        free(utf);
    }
    CoTaskMemFree(source); CoTaskMemFree(message); return S_OK;
}
static HRESULT STDMETHODCALLTYPE sf_navigation(ICoreWebView2NavigationStartingEventHandler *self,
                                               ICoreWebView2 *sender, ICoreWebView2NavigationStartingEventArgs *args) {
    SfView *v = ((SfNavigationHandler *)self)->view; (void)sender; LPWSTR uri = NULL;
    if (FAILED(ICoreWebView2NavigationStartingEventArgs_get_Uri(args, &uri)) || !sf_view_source(v, uri))
        ICoreWebView2NavigationStartingEventArgs_put_Cancel(args, TRUE);
    CoTaskMemFree(uri); return S_OK;
}
static HRESULT STDMETHODCALLTYPE sf_popup(ICoreWebView2NewWindowRequestedEventHandler *self,
                                          ICoreWebView2 *sender, ICoreWebView2NewWindowRequestedEventArgs *args) {
    (void)self; (void)sender; ICoreWebView2NewWindowRequestedEventArgs_put_Handled(args, TRUE); return S_OK;
}
static ICoreWebView2WebMessageReceivedEventHandlerVtbl sf_message_vtable = {SfMessageHandler_query, SfMessageHandler_add, SfMessageHandler_release, sf_message};
static ICoreWebView2NavigationStartingEventHandlerVtbl sf_navigation_vtable = {SfNavigationHandler_query, SfNavigationHandler_add, SfNavigationHandler_release, sf_navigation};
static ICoreWebView2NewWindowRequestedEventHandlerVtbl sf_popup_vtable = {SfPopupHandler_query, SfPopupHandler_add, SfPopupHandler_release, sf_popup};
static HRESULT sf_view_events(SfView *v) {
    HRESULT rc;
    { SF_NEW_HANDLER(SfMessageHandler, sf_message_vtable, v);
      rc = ICoreWebView2_add_WebMessageReceived(v->core, &handler->iface, &v->web_token); SfMessageHandler_release(&handler->iface); if (FAILED(rc)) return rc; }
    { SF_NEW_HANDLER(SfNavigationHandler, sf_navigation_vtable, v);
      rc = ICoreWebView2_add_NavigationStarting(v->core, &handler->iface, &v->nav_token); SfNavigationHandler_release(&handler->iface); if (FAILED(rc)) return rc; }
    { SF_NEW_HANDLER(SfPopupHandler, sf_popup_vtable, v);
      rc = ICoreWebView2_add_NewWindowRequested(v->core, &handler->iface, &v->popup_token); SfPopupHandler_release(&handler->iface); }
    return rc;
}
static HRESULT STDMETHODCALLTYPE sf_controller(ICoreWebView2CreateCoreWebView2ControllerCompletedHandler *self,
                                               HRESULT error, ICoreWebView2Controller *controller) {
    SfView *v = ((SfControllerHandler *)self)->view;
    if (v->closing) { if (controller) ICoreWebView2Controller_Close(controller); return S_OK; }
    if (FAILED(error) || !controller) { fprintf(stderr, "WebView2 controller: %08lx\n", (unsigned long)error); if (v->message) v->message(v->context, "webview-error"); return S_OK; }
    v->controller = controller; ICoreWebView2Controller_AddRef(controller);
    if (FAILED(ICoreWebView2Controller_get_CoreWebView2(controller, &v->core)) || FAILED(sf_view_events(v))) {
        if (v->message) v->message(v->context, "webview-error"); return S_OK;
    }
    ICoreWebView2Settings *settings = NULL;
    if (SUCCEEDED(ICoreWebView2_get_Settings(v->core, &settings))) {
        ICoreWebView2Settings_put_AreDevToolsEnabled(settings, FALSE);
        ICoreWebView2Settings_put_AreDefaultContextMenusEnabled(settings, FALSE);
        ICoreWebView2Settings_put_IsStatusBarEnabled(settings, FALSE);
        ICoreWebView2Settings_put_IsZoomControlEnabled(settings, FALSE);
        ICoreWebView2Settings_Release(settings);
    }
    ICoreWebView2Controller2 *color_controller = NULL;
    if (SUCCEEDED(ICoreWebView2Controller_QueryInterface(controller, &IID_ICoreWebView2Controller2, (void **)&color_controller))) {
        COREWEBVIEW2_COLOR color = {255, 10, 27, 44};
        ICoreWebView2Controller2_put_DefaultBackgroundColor(color_controller, color); ICoreWebView2Controller2_Release(color_controller);
    }
    sf_view_bounds(v); sf_view_visible(v, IsWindowVisible(v->window));
    wchar_t *page = wide((const char *)sf_ui_html);
    if (!page || FAILED(ICoreWebView2_NavigateToString(v->core, page))) {
        if (v->message) v->message(v->context, "webview-error");
    }
    free(page); return S_OK;
}
static ICoreWebView2CreateCoreWebView2ControllerCompletedHandlerVtbl sf_controller_vtable = {SfControllerHandler_query, SfControllerHandler_add, SfControllerHandler_release, sf_controller};
static HRESULT STDMETHODCALLTYPE sf_environment(ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler *self,
                                                HRESULT error, ICoreWebView2Environment *environment) {
    SfView *v = ((SfEnvironmentHandler *)self)->view;
    if (v->closing) return S_OK;
    if (FAILED(error) || !environment) { fprintf(stderr, "WebView2 environment: %08lx\n", (unsigned long)error); if (v->message) v->message(v->context, "webview-error"); return S_OK; }
    SF_NEW_HANDLER(SfControllerHandler, sf_controller_vtable, v);
    HRESULT rc = ICoreWebView2Environment_CreateCoreWebView2Controller(environment, v->window, &handler->iface);
    SfControllerHandler_release(&handler->iface);
    if (FAILED(rc) && v->message) v->message(v->context, "webview-error"); return S_OK;
}
static ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandlerVtbl sf_environment_vtable = {SfEnvironmentHandler_query, SfEnvironmentHandler_add, SfEnvironmentHandler_release, sf_environment};
#undef SF_NEW_HANDLER
static SfView *sf_view_create(HWND window, const char *profile, void (*message)(void *, const char *), void *context) {
    SfView *v = calloc(1, sizeof(*v)); SfEnvironmentHandler *handler = calloc(1, sizeof(*handler));
    if (!v || !handler) { free(v); free(handler); return NULL; }
    v->refs = 1; v->window = window; v->message = message; v->context = context;
    const char *prefix = "data:text/html;charset=utf-8;base64,";
    char *b64 = capture_base64((const char *)sf_ui_html, sizeof(sf_ui_html) - 1);
    char *uri = b64 ? malloc(strlen(prefix) + strlen(b64) + 1) : NULL;
    if (uri) { strcpy(uri, prefix); strcat(uri, b64); v->page_uri = wide(uri); }
    free(b64); free(uri);
    if (!v->page_uri) { free(handler); sf_view_unref(v); return NULL; }
    handler->iface.lpVtbl = &sf_environment_vtable; handler->refs = 1; handler->view = v; sf_view_ref(v);
    wchar_t *folder = wide(profile);
    HRESULT rc = folder ? CreateCoreWebView2EnvironmentWithOptions(NULL, folder, NULL, &handler->iface) : E_OUTOFMEMORY;
    free(folder); SfEnvironmentHandler_release(&handler->iface);
    if (FAILED(rc)) { fprintf(stderr, "WebView2 startup: %08lx\n", (unsigned long)rc); v->closing = 1; sf_view_unref(v); return NULL; }
    return v;
}
static void sf_view_close(SfView *v) {
    if (!v) return;
    v->closing = 1; v->message = NULL; v->context = NULL;
    if (v->core) {
        ICoreWebView2_remove_WebMessageReceived(v->core, v->web_token);
        ICoreWebView2_remove_NavigationStarting(v->core, v->nav_token);
        ICoreWebView2_remove_NewWindowRequested(v->core, v->popup_token);
        ICoreWebView2_Release(v->core); v->core = NULL;
    }
    if (v->controller) { ICoreWebView2Controller_Close(v->controller); ICoreWebView2Controller_Release(v->controller); v->controller = NULL; }
    sf_view_unref(v);
}
#endif
