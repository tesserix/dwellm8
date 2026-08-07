import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, Card, ChevronLeft, ListRow, KeyValue, StatusPill, Avatar, Metric,
  BellIcon, DocIcon, GlobeIcon, RefreshIcon, ShieldIcon, ToolboxIcon,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { earnings, tech } from '../src/data/mock';

/** The technician's own record — rating, documents and how they are paid. */
export default function Profile() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="You"
        showCaret={false}
        left={<Pressable onPress={goBack} hitSlop={10}><ChevronLeft size={28} w={2.4} /></Pressable>}
        right={<Pressable hitSlop={10}><Text style={s.logout}>Log out</Text></Pressable>}
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        <View style={s.hero}>
          <Avatar initials={tech.initials} size={96} tone="green" />
          <Text style={s.name}>{tech.name}</Text>
          <StatusPill text={tech.trade} tone="blue" />
          <Text style={s.detail}>{tech.firm}</Text>
          <Text style={s.detail}>{tech.phone}</Text>
        </View>

        <View style={s.metrics}>
          <Metric value={`★ ${tech.rating}`} label="rating" tone="green" />
          <Metric value={String(tech.jobsDone)} label="jobs done" tone="blue" />
          <Metric value={`${tech.onTimePct}%`} label="on time" tone="violet" />
        </View>

        <Card>
          <Text style={s.h}>How you are paid</Text>
          <KeyValue k="Settled to" v={`${tech.firm} · HDFC ••8821`} />
          <KeyValue k="Cycle" v="Weekly, every Tuesday" />
          <KeyValue k="Next settlement" v={earnings.nextSettlement} />
          <KeyValue k="TDS" v="1% under 194C" last />
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow left={<ToolboxIcon size={20} />} title="Skills and trades" subtitle="Plumbing, appliances, water heaters" onPress={() => {}} />
          <ListRow left={<DocIcon size={20} />} title="Documents" subtitle="Aadhaar verified, police verification on file" onPress={() => {}} />
          <ListRow left={<BellIcon size={20} />} title="Notifications" subtitle="New offers, approvals, settlement" onPress={() => {}} />
          <ListRow left={<RefreshIcon size={20} />} title="Offline queue" subtitle="Nothing waiting" onPress={() => {}} />
          <ListRow left={<GlobeIcon size={20} />} title="Language" subtitle="English · हिन्दी · ಕನ್ನಡ" onPress={() => {}} />
          <ListRow left={<ShieldIcon size={20} />} title="Safety and escalation" subtitle="What to do if a site is unsafe" onPress={() => {}} last />
        </Card>

        <Text style={s.version}>Dwellm8 Pro {tech.version} · demonstration data</Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  logout: { ...font.title, color: color.accent },
  hero: { alignItems: 'center', paddingVertical: space(7), gap: 6 },
  name: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  detail: { ...font.body, color: color.ink },
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginBottom: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  version: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(5) },
});
