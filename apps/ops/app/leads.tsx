import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ChipRow, ListRow, StatusPill, Avatar, Button,
  Metric, Toast, KeyValue, PhoneIcon, ChatIcon, CalendarIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { leads } from '../src/data/mock';

/** Lead to lease — the pipeline as a field agent works it. */

const stageTone: Record<string, Tone> = {
  New: 'blue', Contacted: 'neutral', 'Viewing booked': 'violet', Application: 'amber', Offer: 'green',
};

export default function Leads() {
  const router = useRouter();
  const [stage, setStage] = useState('All');
  const [open, setOpen] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2400);
  };

  const list = stage === 'All' ? leads : leads.filter((l) => l.stage === stage);

  return (
    <>
      <BackHeader title="Leads and viewings" subtitle="1 vacant unit, 4 active leads" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <View style={s.metrics}>
          <Metric value="4" label="active leads" tone="blue" />
          <Metric value="1" label="offer out" tone="green" />
          <Metric value="11 d" label="median days to let" tone="violet" />
        </View>

        <ChipRow
          items={[{ label: 'All' }, { label: 'New' }, { label: 'Viewing booked' }, { label: 'Offer' }]}
          value={stage}
          onChange={setStage}
        />

        {list.map((l) => (
          <Card key={l.id} padded={false} style={{ paddingHorizontal: space(4), paddingVertical: space(2) }}>
            <ListRow
              left={<Avatar initials={l.initials} tone={stageTone[l.stage]} />}
              title={l.name}
              subtitle={l.interest}
              meta={`${l.source} · since ${l.since} · budget ${inr(l.budgetPaise, { noPaise: true })}`}
              right={<StatusPill text={l.stage} tone={stageTone[l.stage]} />}
              onPress={() => setOpen(open === l.id ? null : l.id)}
              last
            />
            {open === l.id ? (
              <View style={{ paddingBottom: space(3) }}>
                <KeyValue k="Phone" v={l.phone} />
                <KeyValue k="Next step" v={l.stage === 'Offer' ? 'Chase the signed offer' : 'Book a viewing'} last />
                <View style={{ flexDirection: 'row', gap: 8, marginTop: space(3) }}>
                  <Button label="Call" tone="secondary" small icon={<PhoneIcon size={16} c={color.accent} />} onPress={() => say('Call logged')} style={{ flex: 1 }} />
                  <Button label="WhatsApp" tone="secondary" small icon={<ChatIcon size={16} c={color.accent} />} onPress={() => say('Message sent')} style={{ flex: 1 }} />
                  <Button label="Viewing" small icon={<CalendarIcon size={16} c="#FFF" />} onPress={() => say('Viewing booked for tomorrow 6 PM')} style={{ flex: 1 }} />
                </View>
              </View>
            ) : null}
          </Card>
        ))}

        <Card>
          <Text style={s.h}>Flat 501, Brigade Palm Grove</Text>
          <Text style={s.body}>
            Vacant since 12 July. Listed at {inr(4_00_00_00, { noPaise: true })} on two portals; 3 enquiries
            this week and one offer at asking. Every day vacant costs the owner {inr(13_33_00, { noPaise: true })}.
          </Text>
          <Button label="Accept the offer from Tanvi Desai" onPress={() => say('Offer accepted — agreement drafting started')} style={{ marginTop: space(4) }} />
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4), marginBottom: space(1) },
  h: { ...font.h3, color: color.inkStrong },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
});
