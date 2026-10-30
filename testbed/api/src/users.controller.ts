import { Controller, Get, Req, UseGuards } from '@nestjs/common';
import { DbService } from './db.service';
import { JwtGuard, JwtUser } from './auth/jwt.guard';

@Controller('users')
export class UsersController {
  constructor(private readonly db: DbService) {}

  @Get('me')
  @UseGuards(JwtGuard)
  me(@Req() req: { user: JwtUser }) {
    // No WHERE clause on purpose: the RLS policy is what limits this to one row.
    return this.db.asUser(req.user.sub, async (c) => {
      const { rows } = await c.query('select id, email, created_at from users');
      return rows[0];
    });
  }
}
