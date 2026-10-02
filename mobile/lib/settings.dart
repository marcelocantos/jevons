import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// The only place the cockpit URL is written down. Everything else reads it
/// through [CockpitSettings], so a transport swap (e.g. to Pigeon) is a
/// settings change on the device, not a code change.
const String kDefaultCockpitUrl = 'https://jevons.canticode.com';

const String _kCockpitUrlKey = 'cockpit_url';

/// Persisted shell settings. One value for now: the cockpit URL.
class CockpitSettings extends ChangeNotifier {
  CockpitSettings._(this._prefs, this._url);

  final SharedPreferences _prefs;
  String _url;

  String get url => _url;

  static Future<CockpitSettings> load() async {
    final prefs = await SharedPreferences.getInstance();
    final stored = prefs.getString(_kCockpitUrlKey);
    final url = (stored == null || stored.trim().isEmpty)
        ? kDefaultCockpitUrl
        : stored.trim();
    return CockpitSettings._(prefs, url);
  }

  Future<void> setUrl(String value) async {
    final next = value.trim().isEmpty ? kDefaultCockpitUrl : value.trim();
    if (next == _url) return;
    _url = next;
    await _prefs.setString(_kCockpitUrlKey, next);
    notifyListeners();
  }

  Future<void> resetUrl() => setUrl(kDefaultCockpitUrl);
}

/// Returns null when [value] is an acceptable cockpit URL, else a message.
String? validateCockpitUrl(String value) {
  final text = value.trim();
  if (text.isEmpty) return null; // empty means "use the default"
  final uri = Uri.tryParse(text);
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
    return 'Enter a full URL, e.g. https://host:port/path';
  }
  if (uri.scheme != 'http' && uri.scheme != 'https') {
    return 'Only http and https URLs are supported';
  }
  return null;
}

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.settings});

  final CockpitSettings settings;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _urlController;
  final _formKey = GlobalKey<FormState>();

  @override
  void initState() {
    super.initState();
    _urlController = TextEditingController(text: widget.settings.url);
  }

  @override
  void dispose() {
    _urlController.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!(_formKey.currentState?.validate() ?? false)) return;
    await widget.settings.setUrl(_urlController.text);
    if (mounted) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: Form(
        key: _formKey,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            TextFormField(
              controller: _urlController,
              decoration: const InputDecoration(
                labelText: 'Cockpit URL',
                hintText: kDefaultCockpitUrl,
                helperText: 'Leave blank to use the default.',
              ),
              keyboardType: TextInputType.url,
              autocorrect: false,
              enableSuggestions: false,
              textInputAction: TextInputAction.done,
              validator: validateCockpitUrl,
              onFieldSubmitted: (_) => _save(),
            ),
            const SizedBox(height: 16),
            Row(
              children: [
                TextButton(
                  onPressed: () =>
                      _urlController.text = kDefaultCockpitUrl,
                  child: const Text('Reset to default'),
                ),
                const Spacer(),
                FilledButton(
                  onPressed: _save,
                  child: const Text('Save and reload'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
