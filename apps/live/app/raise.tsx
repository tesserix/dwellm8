import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, TextInput, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Card, CloseIcon, color, font, inr, radius, space } from '@dwellm8/mobile-shared';
import { raiseTicket, ticketCategories, useLiveData } from '../src/data/source';

/**
 * Raise a request in under 30 seconds (requirements MNT-01) — and tell the
 * tenant who pays before anything is dispatched (MNT-02), which is the whole
 * point of the cost-sharing engine.
 */
export default function Raise() {
  const router = useRouter();
  const { leaseId } = useLiveData();
  const [cat, setCat] = useState(ticketCategories[0]);
  const [urgent, setUrgent] = useState(false);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    if (!leaseId || !text.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      const body = urgent ? `URGENT: ${text.trim()}` : text.trim();
      const title = text.trim().split('\n')[0].slice(0, 80);
      await raiseTicket(leaseId, { category: cat.code, title, body });
      router.back();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

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
          {ticketCategories.map((c) => (
            <Pressable key={c.code} onPress={() => setCat(c)} style={[s.cat, c.code === cat.code && s.catActive]}>
              <Text style={[s.catText, c.code === cat.code && s.catTextActive]}>{c.label}</Text>
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
            Based on your agreement, {cat.label.toLowerCase()} work caused by wear or a defect is
            <Text style={{ fontWeight: '700' }}> owner-borne above {inr(10_00_00)}</Text>. Your manager confirms
            the exact split once it's assessed, and you'll see it before any work is approved.
          </Text>
        </View>

        {error ? <Text style={s.error}>{error}</Text> : null}

        <Pressable
          style={[s.submit, (!text.trim() || !leaseId || busy) && { opacity: 0.5 }]}
          onPress={submit}
          disabled={!text.trim() || !leaseId || busy}
        >
          <Text style={s.submitText}>{busy ? 'Sending…' : 'Submit request'}</Text>
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
  urgentRow: {
    flexDirection: 'row', alignItems: 'center', gap: 14,
    backgroundColor: '#FFF', borderRadius: radius.lg,
    marginHorizontal: space(4), padding: space(4), marginBottom: space(3), marginTop: space(3),
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
  error: { ...font.small, color: '#E0524E', textAlign: 'center', marginBottom: space(3), marginHorizontal: space(4) },
  submit: {
    backgroundColor: color.accent, borderRadius: radius.pill,
    marginHorizontal: space(4), paddingVertical: space(4), alignItems: 'center',
  },
  submitText: { ...font.h3, color: '#FFF' },
});
