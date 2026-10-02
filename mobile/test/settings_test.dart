import 'package:flutter_test/flutter_test.dart';
import 'package:jevons_mobile/settings.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

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
