// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

describeOracle(family('composer-chrome'), () => {
  itOracle.todo('T70', 'composer growth keeps the latest assistant reply readable');
  itOracle.todo('T70.1', 'composer growth does not cover the latest assistant response');
  itOracle.todo('T76', 'pasted images reach the agent turn');
  itOracle.todo('T80', 'Wispr/dictation inserts are lightly tidied');
  itOracle.todo('T123', 'empty composer height matches the send button');
  itOracle.todo('T154', 'send queue survives reload and reconnect');
  itOracle.todo('T183', 'composer draft is visible after reload without an edit keystroke');
  itOracle.todo('T368', 'prefix commands still open when the message starts with image markers');
  itOracle.todo('T478', 'after send, the empty composer stays one control tall');
});
