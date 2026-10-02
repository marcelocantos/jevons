import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:webview_flutter/webview_flutter.dart';

import 'settings.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final settings = await CockpitSettings.load();
  runApp(JevonsMobileApp(settings: settings));
}

class JevonsMobileApp extends StatelessWidget {
  const JevonsMobileApp({super.key, required this.settings});

  final CockpitSettings settings;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Jevons',
      theme: ThemeData(colorSchemeSeed: Colors.indigo, useMaterial3: true),
      darkTheme: ThemeData(
        colorSchemeSeed: Colors.indigo,
        brightness: Brightness.dark,
        useMaterial3: true,
      ),
      home: CockpitShell(settings: settings),
    );
  }
}

/// Full-screen WebView onto the cockpit with no browser chrome: no URL bar,
/// no reload button, no visible settings icon. The shell is a single-purpose
/// app pointed at one cockpit URL, so navigation chrome would only make it
/// feel like a browser. Settings stay reachable through a hidden trigger: a
/// long-press anywhere on the WebView surface. Layout polish for foldables
/// and other form factors is deferred.
class CockpitShell extends StatefulWidget {
  const CockpitShell({super.key, required this.settings});

  final CockpitSettings settings;

  @override
  State<CockpitShell> createState() => _CockpitShellState();
}

class _CockpitShellState extends State<CockpitShell> {
  late final WebViewController _web;
  bool _loading = true;
  String? _loadError;

  @override
  void initState() {
    super.initState();
    _web = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(Colors.black)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageStarted: (_) => setState(() {
            _loading = true;
            _loadError = null;
          }),
          onPageFinished: (_) => setState(() => _loading = false),
          onWebResourceError: (error) {
            // Sub-resource failures are the page's problem; only a failed
            // main-frame load is ours to surface.
            if (error.isForMainFrame ?? true) {
              setState(() {
                _loading = false;
                _loadError = error.description;
              });
            }
          },
        ),
      );
    widget.settings.addListener(_loadCockpit);
    _loadCockpit();
  }

  @override
  void dispose() {
    widget.settings.removeListener(_loadCockpit);
    super.dispose();
  }

  void _loadCockpit() {
    _web.loadRequest(Uri.parse(widget.settings.url));
  }

  Future<void> _openSettings() async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => SettingsScreen(settings: widget.settings),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    // No AppBar: the status bar sits over the WebView's black backdrop, so
    // ask for light status-bar icons explicitly rather than inheriting the
    // theme's AppBar-derived style.
    return AnnotatedRegion<SystemUiOverlayStyle>(
      value: SystemUiOverlayStyle.light,
      child: Scaffold(
        backgroundColor: Colors.black,
        body: SafeArea(
          child: Stack(
            children: [
              // Hidden admin entry: the WebView claims only gestures nobody
              // else wants, so a long-press here wins the arena and opens
              // Settings without any visible control.
              GestureDetector(
                onLongPress: _openSettings,
                child: WebViewWidget(controller: _web),
              ),
              if (_loading) const LinearProgressIndicator(),
              if (_loadError != null)
                Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          'Could not load ${widget.settings.url}',
                          textAlign: TextAlign.center,
                        ),
                        const SizedBox(height: 8),
                        Text(
                          _loadError!,
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                        const SizedBox(height: 16),
                        FilledButton(
                          onPressed: _loadCockpit,
                          child: const Text('Retry'),
                        ),
                        TextButton(
                          onPressed: _openSettings,
                          child: const Text('Change URL'),
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
