import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, ActionBar, Toast,
  ChoiceRow, Timeline, UsersIcon,
  color, font, radius, space,
} from '@dwellm8/mobile-shared';
import { listings } from '../src/data/mock';

/**
 * Book an inspection, and check in at the door.
 *
 * The QR is the point: the lister learns who actually turned up rather than
 * guessing from a paper sheet, and the visitor is a registered user, so nobody
 * is asked for their number three times on a Saturday morning.
 */

export default function Inspect() {
  const router = useRouter();
  const { id, slot } = useLocalSearchParams<{ id?: string; slot?: string }>();
  const l = listings.find((x) => x.id === id) ?? listings[0];
  const [pick, setPick] = useState(slot ?? l.inspections[0]?.id ?? 'i1');
  const [booked, setBooked] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const chosen = l.inspections.find((i) => i.id === pick) ?? l.inspections[0];

  if (booked) {
    return (
      <>
        <BackHeader title="You are booked" subtitle={l.title} onBack={() => router.back()} />
        <Screen>
          <Toast text="Added to your enquiries — we will remind you an hour before" />
          <Card>
            <StatusPill text="Inspection booked" tone="green" dot />
            <Text style={s.when}>{chosen?.when}</Text>
            <Text style={s.where}>{l.title} · {l.locality}</Text>

            {/* A QR that stands in for the real code until check-in is wired. */}
            <View style={s.qrWrap}>
              <View style={s.qr}>
                {Array.from({ length: 49 }).map((_, i) => (
                  <View
                    key={i}
                    style={[
                      s.qrCell,
                      (i * 7 + (i % 5) + (i % 3)) % 3 === 0 ? { backgroundColor: color.inkStrong } : null,
                    ]}
                  />
                ))}
              </View>
              <Text style={s.qrCode}>DW-4471-8823</Text>
            </View>

            <Text style={s.note}>
              Show this at the door. It checks you in, tells the lister you attended, and lets you
              message them afterwards without handing over your number.
            </Text>

            <View style={{ marginTop: space(4) }}>
              <KeyValue k="Registered so far" v={`${(chosen?.registered ?? 0) + 1} people`} />
              <KeyValue k="Bring" v="A photo ID, and your questions" />
              <KeyValue k="Cost to you" v="Nothing, ever" tone="green" last />
            </View>
          </Card>
          <Button label="Done" onPress={() => router.replace('/(tabs)/enquiries')} style={{ marginHorizontal: space(4) }} />
        </Screen>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Book an inspection" subtitle={l.title} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        <Card>
          <Text style={s.h}>Pick a time</Text>
          {l.inspections.map((i, idx) => (
            <ChoiceRow
              key={i.id}
              label={i.when}
              hint={`${i.registered} people registered`}
              selected={pick === i.id}
              onPress={() => setPick(i.id)}
              last={idx === l.inspections.length - 1}
            />
          ))}
          {!l.inspections.length ? (
            <Text style={s.body}>No open slots. Ask the lister and we will hold one for you.</Text>
          ) : null}
        </Card>

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <UsersIcon size={20} />
            <Text style={s.h}>How the visit works</Text>
          </View>
          <Timeline
            items={[
              { at: 'On booking', what: 'You get a QR code and a reminder an hour before' },
              { at: 'At the door', what: 'Scan to check in — no register, no phone number on paper' },
              { at: 'After', what: 'Message the lister from the app, or apply in two taps' },
              { at: 'Either way', what: 'The lister sees who came, and who did not', done: false },
            ]}
          />
        </Card>
      </Screen>

      <ActionBar>
        <Button label="Cancel" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button label="Book it" onPress={() => setBooked(true)} disabled={!l.inspections.length} style={{ flex: 1.6 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  when: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  where: { ...font.body, color: color.inkSoft, marginTop: 4 },
  qrWrap: { alignItems: 'center', marginTop: space(5) },
  qr: {
    width: 168, height: 168, backgroundColor: '#FFF', borderRadius: radius.md,
    borderWidth: 1, borderColor: color.line, flexDirection: 'row', flexWrap: 'wrap',
    padding: 8,
  },
  qrCell: { width: '14.28%', height: '14.28%', backgroundColor: 'transparent' },
  qrCode: { ...font.title, color: color.inkStrong, marginTop: space(3), letterSpacing: 1.5 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(4), lineHeight: 18, textAlign: 'center' },
});
