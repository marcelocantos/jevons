// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('frontier'), () => {
  itOracle('T131', 'sidebar default tab order is Frontier then Transcript', () => {
    const panel = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../components/SidebarPanel.tsx'),
      'utf8',
    );
    const fi = panel.indexOf("id: 'frontier'");
    const ti = panel.indexOf("id: 'transcript'");
    expect(fi).toBeGreaterThanOrEqual(0);
    expect(ti).toBeGreaterThan(fi);
  });

  itOracle.todo('T168', 'Frontier tab loads frontier data without HTTP 404');
  itOracle.todo('T173', 'Frontier table is headerless with abbreviated status and fanout');
  itOracle.todo('T179', 'status glyph is normal case; id/fanout columns are tight');
  itOracle.todo('T181', 'ID/name hover is a rich markdown target card');
  itOracle.todo('T248', 'owner can drag-resize RHS width and the sidebar split');
  itOracle.todo('T280', 'Frontier Graph is owner-readable in one glance after hard-reload');
  itOracle.todo('T294', 'Frontier Graph fills the pane with legible nodes');
  itOracle.todo('T340', 'hierarchical ids fit without ellipsis; gutters are even');
  itOracle.todo('T485', 'hover cards are built on first hover and reused until the row source changes');
});
