// 🎯T1300 Stage A: plant a pi-ai mock on every Cursor catalog row.
// Same trick as Claudia sidecar/testdata/abort-provider.ts: mutate the
// bundled model in place so createSeatAgent({ provider: "cursor", ... })
// without modelOverride still hits the mock. Tools stay real.

import assert from "node:assert/strict";
import { getBundledModel, getBundledModels } from "@oh-my-pi/pi-catalog/models";
import { createMockModel, MockModel, registerMockApi, type MockHandler } from "@oh-my-pi/pi-ai/providers/mock";

export const t1300CursorModel = "composer-2.5";

export function plantCursorCatalogMock(handler: MockHandler): MockModel {
  registerMockApi();
  const rows = getBundledModels("cursor");
  assert.ok(rows.length > 0, "pinned catalog has no cursor models");
  let primary: MockModel | undefined;
  for (const catalog of rows) {
    const mock = createMockModel({ provider: "cursor", id: catalog.id, handler });
    Object.assign(catalog, mock);
    Object.setPrototypeOf(catalog, MockModel.prototype);
    Object.defineProperty(catalog, "calls", { value: mock.calls, enumerable: false });
    if (catalog.id === t1300CursorModel) primary = mock;
  }
  const planted = getBundledModel("cursor", t1300CursorModel);
  assert.ok(planted, `catalog missing cursor/${t1300CursorModel}`);
  assert.equal(planted.api, "mock");
  assert.ok(planted instanceof MockModel);
  assert.ok(primary, `failed to plant cursor/${t1300CursorModel}`);
  return primary;
}
