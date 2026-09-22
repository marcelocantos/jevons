// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import '../composer/ensureLocalStorage';
import type { UploadedImage } from '../composer/images';

/**
 * 🎯T562.3: images pasted into a seat's composer but not yet sent, per agent.
 * Uploads already live server-side under a durable id (POST /api/images), so
 * only the reference is stored — never a blob: URL, which dies with the page.
 * The vanilla cockpit kept these in memory only and had no storage key for
 * them, so there is nothing to migrate.
 */
export const usePendingImages = create<{
  images: Record<string, UploadedImage[]>;
  add: (name: string, added: UploadedImage[]) => void;
  removeAt: (name: string, idx: number) => void;
  set: (name: string, images: UploadedImage[]) => void;
}>()(
  persist(
    (set) => ({
      images: {},
      add: (name, added) => set((s) => ({ images: { ...s.images, [name]: (s.images[name] || []).concat(added) } })),
      removeAt: (name, idx) =>
        set((s) => ({ images: { ...s.images, [name]: (s.images[name] || []).filter((_, i) => i !== idx) } })),
      set: (name, images) => set((s) => ({ images: { ...s.images, [name]: images } })),
    }),
    { name: 'jevons-pending-images' },
  ),
);

/** Store shape for one uploaded image: the durable reference only. */
export function durableImage(img: UploadedImage): UploadedImage {
  return { id: img.id, url: img.url, thumbUrl: img.thumbUrl, marker: img.marker, width: img.width, height: img.height };
}
