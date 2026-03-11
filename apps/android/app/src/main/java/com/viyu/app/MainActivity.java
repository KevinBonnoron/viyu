package com.viyu.app;

import android.graphics.Color;
import android.os.Build;
import android.os.Bundle;
import android.os.SystemClock;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowManager;
import android.webkit.WebChromeClient;
import android.widget.FrameLayout;
import android.widget.Toast;
import androidx.activity.OnBackPressedCallback;
import androidx.core.view.WindowCompat;
import androidx.core.view.WindowInsetsCompat;
import androidx.core.view.WindowInsetsControllerCompat;
import com.getcapacitor.BridgeActivity;
import com.getcapacitor.BridgeWebChromeClient;

public class MainActivity extends BridgeActivity {
    private FrameLayout fullscreenView;
    private WebChromeClient.CustomViewCallback fullscreenCallback;
    private int cutoutMode;
    private long lastBackPressed;
    private Toast exitToast;

    private final OnBackPressedCallback fullscreenBack = new OnBackPressedCallback(false) {
        @Override
        public void handleOnBackPressed() {
            hideFullscreen();
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        if (bridge == null) {
            return;
        }
        getOnBackPressedDispatcher().addCallback(this, new OnBackPressedCallback(true) {
            @Override
            public void handleOnBackPressed() {
                handleAppBack();
            }
        });
        getOnBackPressedDispatcher().addCallback(this, fullscreenBack);
        // Keep Capacitor's permission and dialog handling; only provide its missing fullscreen host.
        bridge.getWebView().setWebChromeClient(new BridgeWebChromeClient(bridge) {
            @Override
            public void onShowCustomView(View view, CustomViewCallback callback) {
                showFullscreen(view, callback);
            }

            @Override
            public void onHideCustomView() {
                hideFullscreen();
            }
        });
        hideSystemBars();
    }

    private void handleAppBack() {
        bridge.getWebView().evaluateJavascript("""
            (() => {
                const menus = document.querySelectorAll('[role="menu"], [role="listbox"]');
                for (let i = menus.length - 1; i >= 0; i--) {
                    const menu = menus[i];
                    if (menu.clientWidth === 0 || menu.clientHeight === 0) {
                        continue;
                    }
                    const target = menu.contains(document.activeElement) ? document.activeElement : menu;
                    target.dispatchEvent(new KeyboardEvent('keydown', {
                        key: 'Escape', code: 'Escape', bubbles: true, cancelable: true
                    }));
                    return 'handled';
                }
                const dialog = document.querySelector('dialog:modal');
                if (dialog) {
                    if (typeof dialog.requestClose === 'function') {
                        dialog.requestClose();
                    } else if (dialog.dispatchEvent(new Event('cancel', { cancelable: true }))) {
                        dialog.close();
                    }
                    return 'handled';
                }
                return location.pathname === '/' ? 'home' : 'back';
            })()
            """, result -> {
                if ("\"home\"".equals(result)) {
                    long now = SystemClock.elapsedRealtime();
                    if (lastBackPressed != 0 && now - lastBackPressed < 2000) {
                        resetExitConfirmation();
                        finish();
                    } else {
                        resetExitConfirmation();
                        lastBackPressed = now;
                        exitToast = Toast.makeText(this, R.string.press_back_again_to_exit, Toast.LENGTH_SHORT);
                        exitToast.show();
                    }
                } else {
                    resetExitConfirmation();
                    if ("\"back\"".equals(result)) {
                        if (bridge.getWebView().canGoBack()) {
                            bridge.getWebView().goBack();
                        } else {
                            bridge.getWebView().evaluateJavascript("location.replace('/')", null);
                        }
                    }
                }
            });
    }

    private void resetExitConfirmation() {
        lastBackPressed = 0;
        if (exitToast != null) {
            exitToast.cancel();
            exitToast = null;
        }
    }

    private void hideSystemBars() {
        WindowInsetsControllerCompat controller = WindowCompat.getInsetsController(getWindow(), getWindow().getDecorView());
        controller.setSystemBarsBehavior(WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE);
        controller.hide(WindowInsetsCompat.Type.systemBars());
    }

    private void showFullscreen(View view, WebChromeClient.CustomViewCallback callback) {
        if (fullscreenView != null) {
            callback.onCustomViewHidden();
            return;
        }
        Window window = getWindow();
        ViewGroup decor = (ViewGroup) window.getDecorView();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            WindowManager.LayoutParams attributes = window.getAttributes();
            cutoutMode = attributes.layoutInDisplayCutoutMode;
            attributes.layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            window.setAttributes(attributes);
        }
        resetExitConfirmation();
        fullscreenView = new FrameLayout(this);
        fullscreenView.setBackgroundColor(Color.BLACK);
        fullscreenView.addView(view, new FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        decor.addView(fullscreenView, new ViewGroup.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        fullscreenCallback = callback;
        bridge.getWebView().setVisibility(View.INVISIBLE);
        fullscreenBack.setEnabled(true);
        hideSystemBars();
    }

    private void hideFullscreen() {
        if (fullscreenView == null) {
            return;
        }
        Window window = getWindow();
        ViewGroup decor = (ViewGroup) window.getDecorView();
        decor.removeView(fullscreenView);
        fullscreenView.removeAllViews();
        fullscreenView = null;
        fullscreenBack.setEnabled(false);
        bridge.getWebView().setVisibility(View.VISIBLE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            WindowManager.LayoutParams attributes = window.getAttributes();
            attributes.layoutInDisplayCutoutMode = cutoutMode;
            window.setAttributes(attributes);
        }
        hideSystemBars();
        WebChromeClient.CustomViewCallback callback = fullscreenCallback;
        fullscreenCallback = null;
        callback.onCustomViewHidden();
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) {
            hideSystemBars();
        }
    }

    @Override
    public void onPause() {
        resetExitConfirmation();
        super.onPause();
    }

    @Override
    public void onDestroy() {
        resetExitConfirmation();
        hideFullscreen();
        super.onDestroy();
    }
}
