import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, TextInput, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Card, CloseIcon, PlusIcon, color, font, inr, radius, space } from '@dwellm8/mobile-shared';
import { categories } from '../src/data/mock';

/**
 * Raise a request in under 30 seconds (requirements MNT-01) — and tell the
 * tenant who pays before anything is dispatched (MNT-02), which is the whole
 * point of the cost-sharing engine.
 */
export default function Raise() {
  const router = useRouter();
  const [cat, setCat] = useState('Plumbing');
  const [urgent, setUrgent] = useState(false);
  const [text, setText] = useState('');
  const photos = 0;

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <SafeAreaView edges={['top']} style={{ backgroundColor: '#FFF' }}>
        <View style={{ padding: space(4) }}>
          <Pressable onPress={() => router.back()} hitSlop={10}><CloseIcon size={26} w={2.2} /></Pressable>
          <Text style={s.title}>Raise a request</Text>
        </View>
      </SafeAreaView>

      <ScrollView contentContainerStyle={{ paddingVertical: space(4), paddingBottom: space(10) }}>
        <Text style={s.label}>What needs attention?</Text>
        <View style={s.cats}>
          {categories.map((c) => (
            <Pressable key={c} onPress={() => setCat(c)} style={[s.cat, c === cat && s.catActive]}>
              <Text style={[s.catText, c === cat && s.catTextActive]}>{c}</Text>
            </Pressable>
          ))}
        </View>

        <Text style={s.label}>Tell us what's happening</Text>
        <Card>
          <TextInput
            style={s.input}
            placeholder="The geyser trips the switch after ten minutes…"
            placeholderTextColor={color.inkFaint}
            value={text}
            onChangeText={setText}
            multiline
          />
        </Card>

        <Text style={s.label}>Photos help a lot</Text>
        <Card>
          <Pressable style={s.photoAdd}>
            <PlusIcon size={22} />
            <Text style={s.photoText}>Add photos</Text>
          </Pressable>
          <Text style={s.photoHint}>{photos} added · they go straight to your manager</Text>
        </Card>

        <Pressable style={s.urgentRow} onPress={() => setUrgent(!urgent)}>
          <View style={[s.check, urgent && s.checkOn]}>{urgent ? <Text style={s.tick}>✓</Text> : null}</View>
          <View style={{ flex: 1 }}>
            <Text style={s.urgentLabel}>This is urgent</Text>
            <Text style={s.urgentHint}>Water, electrical, gas, lift or a safety risk</Text>
          </View>
        </Pressable>

        {/* the disclosure that stops the argument before it starts */}
        <View style={s.liability}>
          <Text style={s.liabilityTitle}>Who pays for this</Text>
          <Text style={s.liabilityBody}>
            Based on your agreement, {cat.toLowerCase()} work caused by wear or a defect is
            <Text style={{ fontWeight: '700' }}> owner-borne above {inr(10_00_00)}</Text>. We'll confirm the exact
            split once your manager has assessed it, and you'll see it before any work is approved.
          </Text>
        </View>

        <Pressable style={s.submit} onPress={() => router.back()}>
          <Text style={s.submitText}>Submit request</Text>
        </Pressable>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  title: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  label: { ...font.label, color: color.inkSoft, marginHorizontal: space(4), marginBottom: space(2), marginTop: space(3) },
  cats: { flexDirection: 'row', flexWrap: 'wrap', gap: 9, paddingHorizontal: space(4), marginBottom: space(2) },
  cat: {
    paddingHorizontal: 15, paddingVertical: 9, borderRadius: radius.pill,
    backgroundColor: '#FFF', borderWidth: 1, borderColor: color.line,
  },
  catActive: { backgroundColor: color.accent, borderColor: color.accent },
  catText: { ...font.label, color: color.accent },
  catTextActive: { color: '#FFF' },
  input: { ...font.body, color: color.inkStrong, minHeight: 96, textAlignVertical: 'top' },
  photoAdd: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 9, paddingVertical: space(3) },
  photoText: { ...font.label, color: color.accent },
  photoHint: { ...font.small, color: color.inkSoft, textAlign: 'center', marginTop: 4 },
  urgentRow: {
    flexDirection: 'row', alignItems: 'center', gap: 14,
    backgroundColor: '#FFF', borderRadius: radius.lg,
    marginHorizontal: space(4), padding: space(4), marginBottom: space(3),
  },
  check: {
    width: 24, height: 24, borderRadius: 7, borderWidth: 2, borderColor: color.accent,
    alignItems: 'center', justifyContent: 'center',
  },
  checkOn: { backgroundColor: color.accent },
  tick: { color: '#FFF', fontWeight: '800' },
  urgentLabel: { ...font.title, color: color.inkStrong },
  urgentHint: { ...font.small, color: color.inkSoft, marginTop: 2 },
  liability: {
    backgroundColor: '#EAF3F7', borderRadius: radius.lg,
    marginHorizontal: space(4), padding: space(4), marginBottom: space(4),
  },
  liabilityTitle: { ...font.title, color: color.accentDeep, marginBottom: 5 },
  liabilityBody: { ...font.body, color: color.ink, lineHeight: 22 },
  submit: {
    backgroundColor: color.accent, borderRadius: radius.pill,
    marginHorizontal: space(4), paddingVertical: space(4), alignItems: 'center',
  },
  submitText: { ...font.h3, color: '#FFF' },
});
