import React from 'react';
import { Text, View, StyleSheet } from 'react-native';
import { color, font, radius, space } from '@dwellm8/mobile-shared';
import { LIVE_EMPTY, mode } from '../data/source';

/**
 * Shown only in live mode: everything below it is the demonstration workload,
 * and saying so is what keeps it from masquerading as record. The banner
 * disappears with the vendor API (dwellm8#104, #105).
 */
export function DemoNotice() {
  if (mode() === 'demo') return null;
  return (
    <View style={s.wrap}>
      <Text style={s.title}>Demonstration data</Text>
      <Text style={s.body}>{LIVE_EMPTY.body}</Text>
    </View>
  );
}

const s = StyleSheet.create({
  wrap: {
    backgroundColor: '#FFF7E6', borderColor: '#F0D9A6', borderWidth: 1,
    borderRadius: radius.md, marginHorizontal: space(4), marginTop: space(3),
    padding: space(4),
  },
  title: { ...font.label, color: '#8A6D1F' },
  body: { ...font.small, color: '#8A6D1F', marginTop: 4, lineHeight: 18 },
});
