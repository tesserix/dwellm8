import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Screen, Card, ListRow, StatusPill, Timeline, Metric,
  color, font, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { useMyFind } from '../../src/data/source';

/** Every home you are talking to somebody about, and where it stands. */

const tone: Record<string, Tone> = {
  'Enquiry sent': 'neutral',
  'Inspection booked': 'blue',
  Attended: 'violet',
  Applied: 'amber',
  'Offer made': 'green',
  'Not proceeding': 'red',
  // The API's pipeline vocabulary (ADR-0019).
  new: 'neutral',
  owner_responded: 'blue',
  scheduled: 'violet',
  completed: 'green',
  closed: 'red',
};

export default function Enquiries() {
  const router = useRouter();
  const { enquiries } = useMyFind();
  const booked = enquiries.filter((e) => e.state === 'scheduled' || e.state === 'Inspection booked').length;
  const applied = enquiries.filter((e) => e.state === 'Applied' || e.state === 'completed').length;

  return (
    <>
      <AppHeader title="Your enquiries" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <View style={s.metrics}>
          <Metric value={String(enquiries.length)} label="homes in play" tone="blue" />
          <Metric value={String(booked)} label="inspections booked" tone="violet" />
          <Metric value={String(applied)} label="moving forward" tone="amber" />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {enquiries.map((e, i) => (
            <ListRow
              key={e.id}
              title={e.title}
              subtitle={e.state.replace(/_/g, ' ')}
              meta={e.when}
              right={<StatusPill text={e.state.replace(/_/g, ' ')} tone={tone[e.state] ?? 'neutral'} />}
              onPress={() => {}}
              last={i === enquiries.length - 1}
            />
          ))}
          {!enquiries.length ? (
            <Text style={s.empty}>Nothing yet — enquire on a home and it appears here.</Text>
          ) : null}
        </Card>

        <Card>
          <Text style={s.h}>What happens after you apply</Text>
          <Timeline
            items={[
              { at: 'Same day', what: 'The manager or owner reviews your application' },
              { at: 'Within 2 days', what: 'A decision, or a request for one more document' },
              { at: 'On acceptance', what: 'Agreement drafted, e-stamped and eSigned' },
              { at: 'Move-in', what: 'Joint inspection with photographs, then the keys', done: false },
            ]}
          />
          <Text style={s.note}>
            You never pay Dwellm8 to apply, to be shown a home, or to be given a decision. If anyone
            asks you for a fee to see a listing here, report it from the listing.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(3) },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(2) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(4), lineHeight: 18 },
});
