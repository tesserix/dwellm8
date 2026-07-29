import React from 'react';
import { View, Text, StyleSheet, Pressable } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Metric, StatusPill, Button, ListRow,
  SectionTitle, KeyValue, MapPinIcon, PhoneIcon, ClockIcon, RouteIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { earnings, jobs, tech } from '../../src/data/mock';

/**
 * The technician's day.
 *
 * One job matters at a time — the next one. It gets the whole card, the call
 * button and the start action; the rest of the day is a list underneath.
 */

const stateTone: Record<string, Tone> = {
  Offered: 'violet', Accepted: 'blue', Travelling: 'amber', 'On site': 'green',
  'Awaiting parts': 'amber', Completed: 'green', Paid: 'neutral',
};

export default function Today() {
  const router = useRouter();
  const todays = jobs.filter((j) => j.window.startsWith('Today'));
  const next = todays.find((j) => j.state === 'Travelling' || j.state === 'On site') ?? todays[0];
  const rest = todays.filter((j) => j.id !== next.id);
  const offers = jobs.filter((j) => j.state === 'Offered').length;

  return (
    <>
      <AppHeader
        title={tech.firm}
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={s.greetWrap}>
          <Text style={s.greet}>{todays.length} jobs today</Text>
          <Text style={s.date}>Wednesday, 29 July · {tech.trade}</Text>
        </View>

        <View style={s.metrics}>
          <Metric value={String(offers)} label="offers waiting" tone="violet" onPress={() => router.push('/(tabs)/offers')} />
          <Metric value={inr(earnings.weekPaise, { noPaise: true })} label="earned this week" tone="green" onPress={() => router.push('/(tabs)/earnings')} />
          <Metric value={`${tech.onTimePct}%`} label="on time" tone="blue" />
        </View>

        <SectionTitle>Next job</SectionTitle>
        <Card>
          <View style={s.top}>
            <StatusPill text={next.state} tone={stateTone[next.state]} dot />
            <StatusPill text={next.priority} tone={next.priority === 'Emergency' ? 'red' : next.priority === 'Urgent' ? 'amber' : 'neutral'} />
            <View style={{ flex: 1 }} />
            <Text style={s.pay}>{next.underAmc ? 'Under AMC' : inr(next.payPaise, { noPaise: true })}</Text>
          </View>

          <Text style={s.title}>{next.title}</Text>
          <Text style={s.unit}>{next.unit}</Text>

          <View style={s.metaRow}>
            <ClockIcon size={17} c={color.inkSoft} />
            <Text style={s.meta}>{next.window}</Text>
          </View>
          <View style={s.metaRow}>
            <MapPinIcon size={17} c={color.inkSoft} />
            <Text style={s.meta}>{next.locality} · {next.distanceKm} km away</Text>
          </View>

          <View style={s.brief}>
            <Text style={s.briefText}>{next.brief}</Text>
          </View>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Button label="Navigate" tone="secondary" onPress={() => {}} style={{ flex: 1 }} />
            <Button label="Call tenant" tone="secondary" icon={<PhoneIcon size={17} c={color.accent} />} onPress={() => {}} style={{ flex: 1.2 }} />
          </View>
          <Button
            label="Start work with OTP"
            onPress={() => router.push(`/otp?id=${next.id}`)}
            style={{ marginTop: space(3) }}
          />
          <Text style={s.otpNote}>
            Work starts only when the tenant reads you their code. It proves you were there, and it
            protects you if the job is later disputed.
          </Text>
        </Card>

        <SectionTitle>Rest of the day</SectionTitle>
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {rest.map((j, i) => (
            <ListRow
              key={j.id}
              title={j.title}
              subtitle={`${j.unit} · ${j.window.replace('Today, ', '')}`}
              meta={`${j.distanceKm} km · ${j.underAmc ? 'under AMC' : inr(j.payPaise, { noPaise: true })}`}
              right={<StatusPill text={j.state} tone={stateTone[j.state]} />}
              onPress={() => router.push(`/job?id=${j.id}`)}
              last={i === rest.length - 1}
            />
          ))}
        </Card>

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <RouteIcon size={20} />
            <Text style={s.h}>Your route</Text>
          </View>
          <KeyValue k="Total driving" v="16.6 km" />
          <KeyValue k="First stop" v="Whitefield, 09:00" />
          <KeyValue k="Last stop" v="Hebbal, 17:00" last />
          <Text style={s.note}>
            Accepting the Hebbal job at 17:00 makes the day 12 km longer. It is still the highest
            paying offer open to you.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  greetWrap: { paddingHorizontal: space(4), paddingTop: space(4), paddingBottom: space(3) },
  greet: { ...font.h1, color: color.inkStrong },
  date: { ...font.body, color: color.inkSoft, marginTop: 3 },
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginBottom: space(2) },
  top: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  pay: { ...font.h3, color: color.positive },
  title: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  unit: { ...font.body, color: color.inkSoft, marginTop: 3 },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 8 },
  meta: { ...font.body, color: color.ink, flex: 1 },
  brief: { backgroundColor: color.cardMuted, borderRadius: 12, padding: space(3), marginTop: space(4) },
  briefText: { ...font.body, color: color.ink, lineHeight: 21 },
  otpNote: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  h: { ...font.h3, color: color.inkStrong },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});
