import React from 'react';
import { View, StyleSheet, useWindowDimensions, Platform } from 'react-native';
import { color } from '../theme/tokens';

/**
 * Mobile-native app shell.
 *
 * Rentora Own is a phone app first. On a real device this is a pass-through —
 * the app fills the screen. On a desktop browser it constrains the app to a
 * phone-width column and centres it, so the web build behaves like the app it
 * is rather than stretching a phone layout across 1700px of monitor.
 *
 * The breakpoint is deliberately generous: tablets in portrait and small
 * laptop windows still get the full-bleed mobile layout, because that is the
 * experience the design was drawn for.
 */

const PHONE_MAX = 520;   // below this, run full-bleed
const SHELL_W = 440;     // the phone column on a wide screen

export function Shell({ children }: { children: React.ReactNode }) {
  const { width, height } = useWindowDimensions();
  const wide = Platform.OS === 'web' && width > PHONE_MAX;

  if (!wide) return <View style={s.full}>{children}</View>;

  return (
    <View style={s.page}>
      <View
        style={[
          s.device,
          { width: SHELL_W, height: Math.min(940, Math.max(600, height - 48)) },
        ]}
      >
        {children}
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  full: { flex: 1, backgroundColor: color.bgTop },
  page: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: '#DCE3EC',
  },
  device: {
    borderRadius: 30,
    overflow: 'hidden',
    backgroundColor: color.bgTop,
    shadowColor: '#3A4756',
    shadowOpacity: 0.24,
    shadowRadius: 34,
    shadowOffset: { width: 0, height: 14 },
  },
});
