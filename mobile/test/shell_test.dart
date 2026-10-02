import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jevons_mobile/main.dart';
import 'package:jevons_mobile/settings.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_flutter_platform_interface/webview_flutter_platform_interface.dart';

// A WebView platform that renders a plain black box and records what the
// shell asked it to load. Only the calls the shell makes are implemented;
// anything else keeps the interface's UnimplementedError, which is what we
// want a new shell feature to trip over here rather than on a device.
class _FakeWebViewPlatform extends WebViewPlatform {
  _FakeController? lastController;

  @override
  PlatformWebViewController createPlatformWebViewController(
    PlatformWebViewControllerCreationParams params,
  ) =>
      lastController = _FakeController(params);

  @override
  PlatformNavigationDelegate createPlatformNavigationDelegate(
    PlatformNavigationDelegateCreationParams params,
  ) =>
      _FakeNavigationDelegate(params);

  @override
  PlatformWebViewWidget createPlatformWebViewWidget(
    PlatformWebViewWidgetCreationParams params,
  ) =>
      _FakeWebViewWidget(params);
}

class _FakeController extends PlatformWebViewController {
  _FakeController(super.params) : super.implementation();

  final List<Uri> loaded = [];

  @override
  Future<void> setJavaScriptMode(JavaScriptMode javaScriptMode) async {}

  @override
  Future<void> setBackgroundColor(Color color) async {}

  @override
  Future<void> setPlatformNavigationDelegate(
    PlatformNavigationDelegate handler,
  ) async {}

  @override
  Future<void> loadRequest(LoadRequestParams params) async {
    loaded.add(params.uri);
  }
}

class _FakeNavigationDelegate extends PlatformNavigationDelegate {
  _FakeNavigationDelegate(super.params) : super.implementation();

  @override
  Future<void> setOnPageStarted(PageEventCallback onPageStarted) async {}

  @override
  Future<void> setOnPageFinished(PageEventCallback onPageFinished) async {}

  @override
  Future<void> setOnWebResourceError(
    WebResourceErrorCallback onWebResourceError,
  ) async {}
}

class _FakeWebViewWidget extends PlatformWebViewWidget {
  _FakeWebViewWidget(super.params) : super.implementation();

  @override
  Widget build(BuildContext context) =>
      const ColoredBox(color: Colors.black, child: SizedBox.expand());
}

void main() {
  late _FakeWebViewPlatform platform;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    platform = _FakeWebViewPlatform();
    WebViewPlatform.instance = platform;
  });

  Future<void> pumpShell(WidgetTester tester) async {
    final settings = await CockpitSettings.load();
    await tester.pumpWidget(JevonsMobileApp(settings: settings));
    await tester.pump();
  }

  testWidgets('launch shows the WebView with no browser chrome', (
    tester,
  ) async {
    await pumpShell(tester);

    expect(find.byType(WebViewWidget), findsOneWidget);
    expect(find.byType(AppBar), findsNothing);
    expect(find.byIcon(Icons.refresh), findsNothing);
    expect(find.byIcon(Icons.settings), findsNothing);
    expect(find.text(kDefaultCockpitUrl), findsNothing);
    expect(find.byType(SettingsScreen), findsNothing);

    // The WebView is the whole screen, not a strip under a toolbar.
    final screen = tester.getSize(find.byType(MaterialApp));
    expect(tester.getSize(find.byType(WebViewWidget)), screen);
  });

  testWidgets('the shell loads the configured cockpit URL', (tester) async {
    await pumpShell(tester);
    expect(platform.lastController?.loaded, [Uri.parse(kDefaultCockpitUrl)]);
  });

  testWidgets('a long-press on the WebView opens Settings', (tester) async {
    await pumpShell(tester);

    await tester.longPress(find.byType(WebViewWidget));
    // Route transition; pumpAndSettle would wait forever on the loading bar.
    await tester.pump(const Duration(seconds: 1));

    expect(find.byType(SettingsScreen), findsOneWidget);
    expect(find.text('Cockpit URL'), findsOneWidget);
  });

  testWidgets('Save and reload with an unchanged URL reloads the cockpit', (
    tester,
  ) async {
    await pumpShell(tester);
    await tester.longPress(find.byType(WebViewWidget));
    await tester.pump(const Duration(seconds: 1));

    await tester.tap(find.text('Save and reload'));
    // One frame to start the pop transition, one to finish it.
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 1));

    expect(find.byType(SettingsScreen), findsNothing);
    expect(platform.lastController?.loaded, [
      Uri.parse(kDefaultCockpitUrl),
      Uri.parse(kDefaultCockpitUrl),
    ]);
  });

  testWidgets('a plain tap on the WebView does not open Settings', (
    tester,
  ) async {
    await pumpShell(tester);

    await tester.tap(find.byType(WebViewWidget));
    await tester.pump(const Duration(seconds: 1));

    expect(find.byType(SettingsScreen), findsNothing);
  });
}
