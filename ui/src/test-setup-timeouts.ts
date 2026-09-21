// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { configure } from '@testing-library/react';

// 🎯T801: waitFor / findBy poll their condition, so a long ceiling only
// matters when the host is starved. Default 1000 ms flaked under load.
configure({ asyncUtilTimeout: 20_000 });
