import React from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, Card, ChevronLeft, ListRow, KeyValue, StatusPill, Avatar, Button,
  BellIcon, DocIcon, GlobeIcon, RefreshIcon, ShieldIcon, UsersIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { org, staff } from '../src/data/mock';

/** Who you are, what you may do, and what the app is holding for you. */
export default function Profile() {
  const router = useRouter();

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="You"
        showCaret={false}
        left={<Pressable onPress={() => router.back()} hitSlop={10}><ChevronLeft size={28} w={2.4} /></Pressable>}
        right={<Pressable hitSlop={10}><Text style={s.logout}>Log out</Text></Pressable>}
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        <View style={s.hero}>
          <Avatar initials={staff.initials} size={96} tone="blue" />
          <Text style={s.name}>{staff.name}</Text>
          <StatusPill text={staff.role} tone="blue" />
          <Text style={s.detail}>{staff.email}</Text>
          <Text style={s.detail}>{staff.phone}</Text>
        </View>

        <Card>
          <Text style={s.h}>What you may do</Text>
          <KeyValue k="Organisation" v={org.name} />
          <KeyValue k="Spend authority" v={`${inr(staff.spendAuthorityPaise, { noPaise: true })} per job`} />
          <KeyValue k="Release payouts" v="Yes, up to ₹5,00,000" />
          <KeyValue k="Change fees or mandates" v="No — web console only" tone="red" />
          <KeyValue k="Portfolios" v={`${org.portfolios.length} assigned`} last />
          <Text style={s.note}>
            Permissions are scoped to this organisation. Switching to another organisation switches
            the whole context — nothing is ever merged across the two.
          </Text>
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow left={<UsersIcon size={20} />} title="Switch portfolio" subtitle={org.portfolios[0].name} onPress={() => router.push('/switcher')} />
          <ListRow left={<BellIcon size={20} />} title="Notifications" subtitle="SLA breaches, failed mandates, owner approvals" onPress={() => {}} />
          <ListRow left={<RefreshIcon size={20} />} title="Offline queue" subtitle="2 items waiting to sync" onPress={() => {}} />
          <ListRow left={<GlobeIcon size={20} />} title="Language" subtitle="English · हिन्दी available" onPress={() => {}} />
          <ListRow left={<ShieldIcon size={20} />} title="Privacy and data" subtitle="What Dwellm8 stores about you" onPress={() => {}} />
          <ListRow left={<DocIcon size={20} />} title="Terms and conditions" onPress={() => {}} last />
        </Card>

        <Button label="Web console" tone="secondary" onPress={() => {}} style={{ marginHorizontal: space(4) }} />
        <Text style={s.version}>Dwellm8 Ops {staff.version} · demonstration data</Text>
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
  version: { ...font.small, color: color.inkFaint, textAlign: 'center', marginTop: space(5) },
});
