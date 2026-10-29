import 'package:flutter_test/flutter_test.dart';
import 'package:jevons_mobile/settings.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final values = <String, String>{};
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .setMockMethodCallHandler(const MethodChannel('jevons/device_store'), (
    call,
  ) async {
    final args = Map<String, dynamic>.from(call.arguments as Map);
    if (call.method == 'get') return values[args['key']];
    if (call.method == 'put') {
      values[args['key'] as String] = args['value'] as String;
      return null;
    }
    throw MissingPluginException();
  });
  setUp(() {
    values.clear();
    CockpitSettings.androidOverrideForTests = true;
  });
  tearDown(() => CockpitSettings.androidOverrideForTests = null);

  test('iOS retains existing settings backend', () async {
    CockpitSettings.androidOverrideForTests = false;
    SharedPreferences.setMockInitialValues({});
    final settings = await CockpitSettings.load();
    expect(await settings.setUrl('https://ios.example.test'), isTrue);
    expect((await CockpitSettings.load()).url, 'https://ios.example.test');
  });

  test('defaults to the canonical cockpit URL', () async {
    final settings = await CockpitSettings.load();
    expect(settings.url, kDefaultCockpitUrl);
  });

  test('persists a changed URL across loads', () async {
    final settings = await CockpitSettings.load();
    await settings.setUrl('http://10.0.0.5:8080/');
    final reloaded = await CockpitSettings.load();
    expect(reloaded.url, 'http://10.0.0.5:8080/');
  });

  test('blank URL falls back to the default', () async {
    final settings = await CockpitSettings.load();
    await settings.setUrl('http://10.0.0.5:8080/');
    await settings.setUrl('   ');
    expect(settings.url, kDefaultCockpitUrl);
  });

  test('setUrl reports whether anything changed', () async {
    final settings = await CockpitSettings.load();
    expect(await settings.setUrl(kDefaultCockpitUrl), isFalse);
    expect(await settings.setUrl('http://10.0.0.5:8080/'), isTrue);
    expect(await settings.setUrl('http://10.0.0.5:8080/'), isFalse);
  });

  test('requestReload notifies listeners without changing the URL', () async {
    final settings = await CockpitSettings.load();
    var notified = 0;
    settings.addListener(() => notified++);
    settings.requestReload();
    expect(notified, 1);
    expect(settings.url, kDefaultCockpitUrl);
  });

  test('validator accepts http(s) and rejects the rest', () {
    expect(validateCockpitUrl(''), isNull);
    expect(validateCockpitUrl('https://jevons.canticode.com'), isNull);
    expect(validateCockpitUrl('http://192.168.1.10:9000/ui'), isNull);
    expect(validateCockpitUrl('jevons.canticode.com'), isNotNull);
    expect(validateCockpitUrl('ftp://host'), isNotNull);
    expect(validateCockpitUrl('not a url'), isNotNull);
  });
}
