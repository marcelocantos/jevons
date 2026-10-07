import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import 'package:webview_flutter_android/webview_flutter_android.dart';
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
/// feel like a browser. There is no long-press (or other hidden gesture) to
/// Settings — owner 2026-10-03: that path was accidental UX; cockpit settings
/// belong as a control in the web UI when needed. Failed main-frame loads
/// still offer Change URL as a recovery path. Layout polish for foldables
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
  final ImagePicker _imagePicker = ImagePicker();

  @override
  void initState() {
    super.initState();
    _web = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setBackgroundColor(Colors.black)
      // WKWebView has no file-input delegate exposed by webview_flutter.
      // The composer asks this channel to pick on iOS; Android supports the
      // same channel and also handles ordinary HTML file inputs below.
      ..addJavaScriptChannel(
        'JevonsImagePicker',
        onMessageReceived: (message) => _pickForComposer(message.message),
      )
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
    if (_web.platform is AndroidWebViewController) {
      (_web.platform as AndroidWebViewController)
          .setOnShowFileSelector(_selectWebViewFiles);
    }
    widget.settings.addListener(_loadCockpit);
    _loadCockpit();
  }

  @override
  void dispose() {
    widget.settings.removeListener(_loadCockpit);
    super.dispose();
  }

  /// A single native picker for both the HTML chooser (Android) and the
  /// explicit JS channel (iOS and Android). Cancellation returns no asset.
  Future<XFile?> _pickImage({bool capture = false}) async {
    if (!mounted) return null;
    ImageSource? source;
    if (capture) {
      source = ImageSource.camera;
    } else {
      source = await showModalBottomSheet<ImageSource>(
        context: context,
        builder: (context) => SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                leading: const Icon(Icons.photo_library),
                title: const Text('Photo library'),
                onTap: () => Navigator.pop(context, ImageSource.gallery),
              ),
              ListTile(
                leading: const Icon(Icons.camera_alt),
                title: const Text('Camera'),
                onTap: () => Navigator.pop(context, ImageSource.camera),
              ),
            ],
          ),
        ),
      );
    }
    if (source == null || !mounted) return null;
    try {
      return await _imagePicker.pickImage(source: source);
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not pick image: $error')),
        );
      }
      return null;
    }
  }

  Future<List<String>> _selectWebViewFiles(FileSelectorParams params) async {
    if (params.mode == FileSelectorMode.save) return [];
    final image = await _pickImage(capture: params.isCaptureEnabled);
    return image == null ? [] : [Uri.file(image.path).toString()];
  }

  Future<void> _pickForComposer(String request) async {
    // Never interpolate untrusted JS into the page. JSON-encode all values.
    String id;
    try {
      final parsed = jsonDecode(request) as Map<String, dynamic>;
      id = parsed['composerId'] as String;
    } catch (_) {
      return;
    }
    final image = await _pickImage();
    if (image == null || !mounted) return;
    final bytes = await image.readAsBytes();
    final ext = image.name.split('.').last.toLowerCase();
    final mime = switch (ext) {
      'jpg' || 'jpeg' => 'image/jpeg',
      'png' => 'image/png',
      'gif' => 'image/gif',
      'webp' => 'image/webp',
      'heic' => 'image/heic',
      _ => 'image/jpeg', // image_picker may return an extensionless JPEG.
    };
    final payload = jsonEncode({
      'composerId': id,
      'name': image.name,
      'mime': mime,
      'base64': base64Encode(bytes),
    });
    await _web.runJavaScript(
      'window.dispatchEvent(new CustomEvent("jevons-picked-image", {detail: $payload}));',
    );
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
              WebViewWidget(controller: _web),
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
