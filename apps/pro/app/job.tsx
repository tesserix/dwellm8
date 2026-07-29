import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, Timeline, PhotoStrip,
  ActionBar, Toast, PhoneIcon, MapPinIcon, ClockIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { jobs } from '../src/data/mock';

/** One job, everything the technician needs before knocking on the door. */

const stateTone: Record<string, Tone> = {
  Offered: 'violet', Accepted: 'blue', Travelling: 'amber', 'On site': 'green',
  'Awaiting parts': 'amber', Completed: 'green', Paid: 'neutral',
};

export default function JobScreen() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const j = jobs.find((x) => x.id === id) ?? jobs[0];
  const [toast, setToast] = useState<string | null>(null);
  const finished = j.state === 'Completed' || j.state === 'Paid';

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2400);
  };

  return (
    <>
      <BackHeader title={j.title} subtitle={`${j.id.toUpperCase()} · ${j.unit}`} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={j.state} tone={stateTone[j.state]} dot />
            <StatusPill text={j.priority} tone={j.priority === 'Emergency' ? 'red' : j.priority === 'Urgent' ? 'amber' : 'neutral'} />
            <View style={{ flex: 1 }} />
            <Text style={s.pay}>{j.underAmc ? 'Under AMC' : inr(j.payPaise, { noPaise: true })}</Text>
          </View>

          <View style={s.metaRow}>
            <ClockIcon size={17} c={color.inkSoft} />
            <Text style={s.meta}>{j.window}</Text>
          </View>
          <View style={s.metaRow}>
            <MapPinIcon size={17} c={color.inkSoft} />
            <Text style={s.meta}>{j.locality} · {j.distanceKm} km</Text>
          </View>

          <View style={s.brief}><Text style={s.briefText}>{j.brief}</Text></View>

          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Contact" v={j.contact} />
            <KeyValue k="Phone" v={j.contactPhone} />
            <KeyValue k="Access" v={j.access} last />
          </View>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Button label="Call" tone="secondary" icon={<PhoneIcon size={17} c={color.accent} />} onPress={() => say('Calling through a masked number')} style={{ flex: 1 }} />
            <Button label="Navigate" tone="secondary" onPress={() => {}} style={{ flex: 1 }} />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>What the tenant sent</Text>
          <PhotoStrip count={j.photosBefore} />
          {finished ? (
            <>
              <Text style={[s.h, { marginTop: space(4) }]}>Your completion photos</Text>
              <PhotoStrip count={j.photosAfter} />
            </>
          ) : null}
        </Card>

        <Card>
          <Text style={s.h}>Timeline</Text>
          <Timeline items={j.timeline} />
        </Card>

        {finished ? (
          <Card>
            <Text style={s.h}>Settlement</Text>
            <KeyValue k="Job value" v={inr(j.payPaise)} />
            <KeyValue k="TDS (194C, 1%)" v={`− ${inr(Math.round(j.payPaise * 0.01))}`} tone="red" />
            <KeyValue k="Net to your firm" v={inr(j.payPaise - Math.round(j.payPaise * 0.01))} tone="green" last />
          </Card>
        ) : null}
      </Screen>

      {!finished ? (
        <ActionBar>
          <Button label="Need parts" tone="secondary" onPress={() => say('Manager told — job on hold for parts')} style={{ flex: 1 }} />
          <Button label="Start with OTP" onPress={() => router.push(`/otp?id=${j.id}`)} style={{ flex: 1.4 }} />
        </ActionBar>
      ) : null}
    </>
  );
}

const s = StyleSheet.create({
  pay: { ...font.h3, color: color.positive },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 10 },
  meta: { ...font.body, color: color.ink, flex: 1 },
  brief: { backgroundColor: color.cardMuted, borderRadius: 12, padding: space(3), marginTop: space(4) },
  briefText: { ...font.body, color: color.ink, lineHeight: 21 },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
});
