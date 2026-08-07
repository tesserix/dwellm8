import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, Card, ChevronLeft, ListRow, KeyValue, StatusPill, Avatar, SwitchRow,
  BellIcon, DocIcon, ShieldIcon, ChartIcon,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { admin, webOnly } from '../src/data/mock';

/** Who is on call, and the boundary between this app and the console. */
export default function Profile() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const [onCall, setOnCall] = React.useState(admin.onCall);

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
          <Avatar initials={admin.initials} size={96} tone="violet" />
          <Text style={s.name}>{admin.name}</Text>
          <StatusPill text={admin.role} tone="violet" />
          <Text style={s.detail}>{admin.email}</Text>
        </View>

        <Card>
          <SwitchRow
            label="On call"
            hint={onCall ? `Primary rota until ${admin.onCallUntil}` : 'Alerts will not page you'}
            value={onCall}
            onChange={setOnCall}
            last
          />
        </Card>

        <Card>
          <Text style={s.h}>What this app may do</Text>
          <KeyValue k="Acknowledge and intervene" v="Yes" tone="green" />
          <KeyValue k="Approve onboarding and exceptions" v="Yes, with a reason" tone="green" />
          <KeyValue k="Route disputes" v="Yes" tone="green" />
          <KeyValue k="Change fees or rule tables" v="No — console only" tone="red" />
          <KeyValue k="Bulk corrections" v="No — console only" tone="red" last />
          <Text style={s.note}>
            The app and the console share one permission model and one audit trail. The split is by
            the nature of the task, not by who you are.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>On the console</Text>
          {webOnly.map((w) => <Text key={w} style={s.bullet}>· {w}</Text>)}
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow left={<BellIcon size={20} />} title="Paging preferences" subtitle="P1 pages, P2 notifies, P3 digest" onPress={() => {}} />
          <ListRow left={<ChartIcon size={20} />} title="Dashboards" subtitle="Collections, mandates, payouts" onPress={() => {}} />
          <ListRow left={<ShieldIcon size={20} />} title="Your audit trail" subtitle="Every action you have taken" onPress={() => {}} />
          <ListRow left={<DocIcon size={20} />} title="Runbooks" onPress={() => {}} last />
        </Card>

        <Text style={s.version}>Dwellm8 Admin {admin.version} · demonstration data</Text>
      </ScrollView>
    </View>
  );
}

const s = StyleSheet.create({
  logout: { ...font.title, color: color.accent },
  hero: { alignItems: 'center', paddingVertical: space(7), gap: 6 },
  name: { ...font.h1, color: color.inkStrong, marginTop: space(3) },
  detail: { ...font.body, color: color.ink },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  bullet: { ...font.body, color: color.ink, marginTop: 8 },
  version: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(5) },
});
