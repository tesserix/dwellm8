import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Button, ActionBar, Toast, KeyValue, StatusPill,
  color, font, radius, space,
} from '@dwellm8/mobile-shared';
import { jobs } from '../src/data/mock';

/**
 * Start-of-work OTP.
 *
 * The tenant reads out a four-digit code. It is the only way work can start,
 * which is what makes the timestamp, the photos and the eventual invoice
 * defensible if anyone later disputes that the visit happened.
 */

export default function Otp() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const j = jobs.find((x) => x.id === id) ?? jobs[0];

  const [code, setCode] = useState('');
  const [error, setError] = useState(false);
  const [started, setStarted] = useState(false);

  const press = (d: string) => {
    setError(false);
    if (d === '⌫') return setCode((c) => c.slice(0, -1));
    if (code.length >= 4) return;
    const next = code + d;
    setCode(next);
    if (next.length === 4) {
      if (next === j.startCode) setTimeout(() => setStarted(true), 250);
      else setTimeout(() => { setError(true); setCode(''); }, 250);
    }
  };

  if (started) {
    return (
      <>
        <BackHeader title="Work started" onBack={() => router.back()} />
        <Screen>
          <Toast text="Clock running — the manager and tenant can see it" />
          <Card>
            <StatusPill text="On site" tone="green" dot />
            <Text style={s.title}>{j.title}</Text>
            <Text style={s.sub}>{j.unit}</Text>
            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Started" v="Today, 09:04" />
              <KeyValue k="Verified by" v={`${j.contact}'s code`} />
              <KeyValue k="Window" v={j.window} last />
            </View>
          </Card>
          <Card>
            <Text style={s.h}>Before you leave</Text>
            <Text style={s.body}>
              Photograph the finished work from the same angle as the tenant's photos, note any part
              you fitted, and get the sign-off. A job without evidence cannot be settled.
            </Text>
          </Card>
          <Button label="Complete the job" onPress={() => router.replace(`/complete?id=${j.id}`)} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Start work" subtitle={j.unit} onBack={() => router.back()} />
      <Screen scroll={false}>
        <View style={{ flex: 1, paddingTop: space(6) }}>
          <Text style={s.ask}>Ask {j.contact.split(' ')[0]} for the four-digit code</Text>
          <Text style={s.askSub}>It is in their dwellm8 app under this job</Text>

          <View style={s.dots}>
            {[0, 1, 2, 3].map((i) => (
              <View
                key={i}
                style={[
                  s.dot,
                  code.length > i && { backgroundColor: color.accent, borderColor: color.accent },
                  error && { borderColor: color.negative },
                ]}
              />
            ))}
          </View>
          {error ? <Text style={s.err}>That code is not right. Ask them to read it again.</Text> : null}

          <View style={s.pad}>
            {['1', '2', '3', '4', '5', '6', '7', '8', '9', '', '0', '⌫'].map((d, i) =>
              d ? (
                <Pressable
                  key={i}
                  accessibilityRole="button"
                  accessibilityLabel={d === '⌫' ? 'Delete' : d}
                  style={s.key}
                  onPress={() => press(d)}
                >
                  <Text style={s.keyText}>{d}</Text>
                </Pressable>
              ) : (
                <View key={i} style={s.key} />
              ),
            )}
          </View>

          <Text style={s.hint}>Demonstration code for this job: {j.startCode}</Text>
        </View>
      </Screen>

      <ActionBar>
        <Button label="Tenant not available" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  ask: { ...font.h2, color: color.inkStrong, textAlign: 'center', paddingHorizontal: space(6) },
  askSub: { ...font.body, color: color.inkSoft, textAlign: 'center', marginTop: 6 },
  dots: { flexDirection: 'row', justifyContent: 'center', gap: 16, marginTop: space(7) },
  dot: { width: 20, height: 20, borderRadius: 10, borderWidth: 2, borderColor: color.lineDotted },
  err: { ...font.small, color: color.negative, textAlign: 'center', marginTop: space(4) },
  pad: {
    flexDirection: 'row', flexWrap: 'wrap', justifyContent: 'center',
    gap: 14, marginTop: space(7), paddingHorizontal: space(6),
  },
  key: {
    width: 74, height: 62, borderRadius: radius.md, backgroundColor: '#FFF',
    alignItems: 'center', justifyContent: 'center', borderWidth: 1, borderColor: color.line,
  },
  keyText: { fontSize: 24, fontWeight: '700', color: color.inkStrong },
  hint: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(6) },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.body, color: color.inkSoft, marginTop: 3 },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
});
